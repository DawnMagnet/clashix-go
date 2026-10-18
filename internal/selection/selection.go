// Package selection applies node selection specs to mihomo proxy groups.
package selection

import (
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/metacubex/mihomo/adapter/outboundgroup"
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/tunnel"
)

// Spec is a parsed node selection instruction.
type Spec struct {
	Group   string
	Pattern string

	groupMatcher   *matcher
	patternMatcher *matcher
}

type matcher struct {
	raw      string
	re       *regexp.Regexp
	terms    []string
	sequence string
}

// ParseSpecs parses selection specs. A spec without '=' applies to the
// primary selector group; GROUP=PATTERN targets a group matched by GROUP.
func ParseSpecs(raw []string) ([]Spec, error) {
	specs := make([]Spec, 0, len(raw))
	for _, item := range raw {
		spec, err := parseSpec(item)
		if err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

// Validate checks spec syntax without applying it.
func Validate(raw []string) error {
	_, err := ParseSpecs(raw)
	return err
}

func parseSpec(raw string) (Spec, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return Spec{}, fmt.Errorf("empty node selection spec")
	}

	spec := Spec{}
	pattern := value
	// re: patterns may contain '=', so only split a group prefix outside the
	// regex form.
	if !hasRegexPrefix(value) {
		if index := strings.Index(value, "="); index >= 0 {
			spec.Group = strings.TrimSpace(value[:index])
			pattern = strings.TrimSpace(value[index+1:])
			if spec.Group == "" {
				return Spec{}, fmt.Errorf("node selection %q has an empty group name", raw)
			}
			groupMatcher, err := newMatcher(spec.Group)
			if err != nil {
				return Spec{}, fmt.Errorf("node selection group %q: %w", spec.Group, err)
			}
			spec.groupMatcher = groupMatcher
		}
	}
	if pattern == "" {
		return Spec{}, fmt.Errorf("node selection %q has an empty pattern", raw)
	}
	patternMatcher, err := newMatcher(pattern)
	if err != nil {
		return Spec{}, fmt.Errorf("node selection pattern %q: %w", pattern, err)
	}
	spec.Pattern = pattern
	spec.patternMatcher = patternMatcher
	return spec, nil
}

func hasRegexPrefix(value string) bool {
	return len(value) >= 3 && strings.EqualFold(value[:3], "re:")
}

func newMatcher(pattern string) (*matcher, error) {
	m := &matcher{raw: pattern}
	if hasRegexPrefix(pattern) {
		expression := strings.TrimSpace(pattern[3:])
		if expression == "" {
			return nil, fmt.Errorf("regular expression is empty")
		}
		compiled, err := regexp.Compile("(?i)" + expression)
		if err != nil {
			return nil, fmt.Errorf("invalid regular expression: %w", err)
		}
		m.re = compiled
		return m, nil
	}

	m.terms = strings.Fields(strings.ToLower(pattern))
	var sequence strings.Builder
	for _, r := range pattern {
		if unicode.IsSpace(r) {
			continue
		}
		sequence.WriteRune(unicode.ToLower(r))
	}
	m.sequence = sequence.String()
	if m.sequence == "" {
		return nil, fmt.Errorf("pattern is empty")
	}
	return m, nil
}

func (m *matcher) matches(candidate string) bool {
	if m.re != nil {
		return m.re.MatchString(candidate)
	}
	if m.termsMatch(candidate) {
		return true
	}
	return isSubsequence(m.sequence, strings.ToLower(candidate))
}

// match applies the matcher to a candidate list. Substring terms are tried
// across the whole list first; the fuzzy subsequence pass only runs when no
// candidate matched a term.
func (m *matcher) match(candidates []string) (string, bool) {
	if m.re != nil {
		for _, candidate := range candidates {
			if m.re.MatchString(candidate) {
				return candidate, true
			}
		}
		return "", false
	}
	if len(m.terms) > 0 {
		for _, candidate := range candidates {
			if m.termsMatch(candidate) {
				return candidate, true
			}
		}
	}
	for _, candidate := range candidates {
		if isSubsequence(m.sequence, strings.ToLower(candidate)) {
			return candidate, true
		}
	}
	return "", false
}

func (m *matcher) termsMatch(candidate string) bool {
	if len(m.terms) == 0 {
		return false
	}
	lower := strings.ToLower(candidate)
	for _, term := range m.terms {
		if !strings.Contains(lower, term) {
			return false
		}
	}
	return true
}

func isSubsequence(needle, haystack string) bool {
	needleRunes := []rune(needle)
	if len(needleRunes) == 0 {
		return false
	}
	index := 0
	for _, r := range haystack {
		if r == needleRunes[index] {
			index++
			if index == len(needleRunes) {
				return true
			}
		}
	}
	return false
}

type groupInfo struct {
	name    string
	kind    string
	members []string
}

func configGroups(cfg map[string]any) []groupInfo {
	items := anySlice(cfg["proxy-groups"])
	groups := make([]groupInfo, 0, len(items))
	for _, item := range items {
		mapping, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := mapping["name"].(string)
		kind, _ := mapping["type"].(string)
		if name == "" {
			continue
		}
		groups = append(groups, groupInfo{
			name:    name,
			kind:    strings.ToLower(strings.TrimSpace(kind)),
			members: stringSlice(mapping["proxies"]),
		})
	}
	return groups
}

func anySlice(value any) []any {
	switch value := value.(type) {
	case []any:
		return value
	case []string:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = item
		}
		return out
	case []map[string]any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = item
		}
		return out
	default:
		return nil
	}
}

func stringSlice(value any) []string {
	switch value := value.(type) {
	case []string:
		return append([]string(nil), value...)
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func primaryGroupName(cfg map[string]any, groups []groupInfo) string {
	if target := finalMatchTarget(cfg); target != "" {
		for _, group := range groups {
			if group.name == target && group.kind == "select" {
				return target
			}
		}
	}
	for _, group := range groups {
		if group.kind == "select" {
			return group.name
		}
	}
	return ""
}

func finalMatchTarget(cfg map[string]any) string {
	target := ""
	for _, rule := range stringSlice(cfg["rules"]) {
		parts := strings.Split(rule, ",")
		if len(parts) < 2 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(parts[0]), "MATCH") {
			target = strings.TrimSpace(parts[1])
		}
	}
	return target
}

func selectGroupByName(groups []groupInfo, name string) (groupInfo, bool) {
	for _, group := range groups {
		if group.kind == "select" && group.name == name {
			return group, true
		}
	}
	return groupInfo{}, false
}

func matchGroup(groups []groupInfo, m *matcher) (string, error) {
	if m == nil {
		return "", fmt.Errorf("missing group matcher")
	}
	candidates := make([]string, 0, len(groups))
	for _, group := range groups {
		if group.kind == "select" {
			candidates = append(candidates, group.name)
		}
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("no select proxy group found")
	}
	if name, ok := m.match(candidates); ok {
		return name, nil
	}
	return "", fmt.Errorf("no select proxy group matches %q; closest groups: %s", m.raw, strings.Join(closest(m.raw, candidates, 5), ", "))
}

// Apply applies selection specs to running mihomo groups and then skips
// placeholder nodes for select groups that were not explicitly targeted.
func Apply(cfg map[string]any, raw []string) error {
	specs, err := ParseSpecs(raw)
	if err != nil {
		return err
	}
	return ApplyParsed(cfg, specs)
}

// ApplyParsed applies already parsed specs. Bare patterns run first across all
// select groups; precise GROUP=PATTERN specs run afterwards and win.
func ApplyParsed(cfg map[string]any, specs []Spec) error {
	if len(specs) == 0 {
		return applyPlaceholderDefaults(cfg, nil)
	}

	groups := configGroups(cfg)
	proxies := tunnel.Proxies()
	primary := primaryGroupName(cfg, groups)
	targeted := make(map[string]bool)

	for _, spec := range specs {
		if spec.Group != "" {
			continue
		}
		if err := applyBarePattern(spec, groups, proxies, primary, targeted); err != nil {
			return err
		}
	}
	for _, spec := range specs {
		if spec.Group == "" {
			continue
		}
		groupName, err := matchGroup(groups, spec.groupMatcher)
		if err != nil {
			return err
		}
		if err := applyToGroup(spec, groupName, proxies, targeted); err != nil {
			return err
		}
	}

	return applyPlaceholderDefaults(cfg, targeted)
}

func applyBarePattern(spec Spec, groups []groupInfo, proxies map[string]C.Proxy, primary string, targeted map[string]bool) error {
	if len(groups) == 0 {
		return fmt.Errorf("no select proxy group found for node selection %q", spec.Pattern)
	}

	type plan struct {
		group    string
		selected string
	}
	var plans []plan
	matched := 0
	for _, info := range groups {
		if info.kind != "select" {
			continue
		}
		proxy, ok := proxies[info.name]
		if !ok {
			continue
		}
		members := groupMembers(proxy)
		if len(members) == 0 {
			continue
		}
		names := memberNames(members)
		if selected, ok := spec.patternMatcher.match(names); ok {
			plans = append(plans, plan{group: info.name, selected: selected})
			matched++
			continue
		}
		if info.name != primary && primary != "" && containsMember(members, primary) {
			plans = append(plans, plan{group: info.name, selected: primary})
		}
	}

	if matched == 0 {
		if primary == "" {
			return fmt.Errorf("no select proxy group found for node selection %q", spec.Pattern)
		}
		candidates := primaryCandidates(primary, proxies, groups)
		return fmt.Errorf("no proxy in primary group %q matches %q; closest candidates: %s", primary, spec.Pattern, strings.Join(closest(spec.Pattern, candidates, 5), ", "))
	}

	for _, plan := range plans {
		proxy, ok := proxies[plan.group]
		if !ok {
			return fmt.Errorf("proxy group %q is not available", plan.group)
		}
		if err := setSelected(proxy, plan.selected); err != nil {
			return fmt.Errorf("select %q in group %q: %w", plan.selected, plan.group, err)
		}
		targeted[plan.group] = true
	}
	return nil
}

func applyToGroup(spec Spec, groupName string, proxies map[string]C.Proxy, targeted map[string]bool) error {
	proxy, ok := proxies[groupName]
	if !ok {
		return fmt.Errorf("proxy group %q is not available", groupName)
	}
	members := groupMembers(proxy)
	if len(members) == 0 {
		return fmt.Errorf("proxy group %q has no members", groupName)
	}
	names := memberNames(members)
	selected, ok := spec.patternMatcher.match(names)
	if !ok {
		return fmt.Errorf("no proxy in group %q matches %q; closest candidates: %s", groupName, spec.Pattern, strings.Join(closest(spec.Pattern, names, 5), ", "))
	}
	if err := setSelected(proxy, selected); err != nil {
		return fmt.Errorf("select %q in group %q: %w", selected, groupName, err)
	}
	targeted[groupName] = true
	return nil
}

func memberNames(members []C.Proxy) []string {
	names := make([]string, 0, len(members))
	for _, member := range members {
		names = append(names, member.Name())
	}
	return names
}

func containsMember(members []C.Proxy, name string) bool {
	for _, member := range members {
		if member.Name() == name {
			return true
		}
	}
	return false
}

func primaryCandidates(primary string, proxies map[string]C.Proxy, groups []groupInfo) []string {
	if proxy, ok := proxies[primary]; ok {
		if names := memberNames(groupMembers(proxy)); len(names) > 0 {
			return names
		}
	}
	if group, ok := selectGroupByName(groups, primary); ok {
		return append([]string(nil), group.members...)
	}
	return nil
}

func groupMembers(proxy C.Proxy) []C.Proxy {
	group, ok := proxy.Adapter().(outboundgroup.ProxyGroup)
	if !ok {
		return nil
	}
	return group.Proxies()
}

func setSelected(proxy C.Proxy, name string) error {
	selectable, ok := proxy.Adapter().(outboundgroup.SelectAble)
	if !ok {
		return fmt.Errorf("group does not support selection")
	}
	return selectable.Set(name)
}

func applyPlaceholderDefaults(cfg map[string]any, targeted map[string]bool) error {
	proxies := tunnel.Proxies()
	for _, info := range configGroups(cfg) {
		if info.kind != "select" || targeted[info.name] {
			continue
		}
		proxy, ok := proxies[info.name]
		if !ok {
			continue
		}
		group, ok := proxy.Adapter().(outboundgroup.ProxyGroup)
		if !ok {
			continue
		}
		members := group.Proxies()
		if len(members) == 0 {
			continue
		}
		selected := group.Now()
		current := members[0]
		for _, member := range members {
			if member.Name() == selected {
				current = member
				break
			}
		}
		if !isPlaceholder(current) {
			continue
		}
		chosen := ""
		for _, member := range members {
			if !isPlaceholder(member) {
				chosen = member.Name()
				break
			}
		}
		if chosen == "" || chosen == current.Name() {
			continue
		}
		if err := setSelected(proxy, chosen); err != nil {
			return fmt.Errorf("skip placeholder in group %q: %w", info.name, err)
		}
	}
	return nil
}

var placeholderServers = map[string]struct{}{
	"":               {},
	"10.255.255.255": {},
	"0.0.0.0":        {},
	"127.0.0.1":      {},
	"::1":            {},
}

var placeholderNames = []string{
	"剩余流量",
	"套餐到期",
	"到期",
	"重置",
	"流量",
	"官网",
	"客服",
	"expire",
	"traffic",
	"remaining",
	"reset",
}

func isPlaceholder(proxy C.Proxy) bool {
	if _, group := proxy.Adapter().(outboundgroup.ProxyGroup); group {
		return false
	}
	switch proxy.Adapter().Type() {
	case C.Direct, C.Reject, C.RejectDrop:
		return false
	}
	return isPlaceholderInfo(proxy.Name(), proxy.Adapter().Addr())
}

func isPlaceholderInfo(name, address string) bool {
	host := address
	if parsedHost, _, err := net.SplitHostPort(address); err == nil {
		host = parsedHost
	}
	host = strings.Trim(host, "[]")
	if _, placeholder := placeholderServers[host]; placeholder {
		return true
	}
	lower := strings.ToLower(name)
	for _, needle := range placeholderNames {
		if strings.Contains(lower, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}

func closest(pattern string, candidates []string, limit int) []string {
	type scored struct {
		name  string
		score int
		order int
	}
	scoredCandidates := make([]scored, 0, len(candidates))
	for index, candidate := range candidates {
		scoredCandidates = append(scoredCandidates, scored{
			name:  candidate,
			score: editDistance(normalizePattern(pattern), strings.ToLower(candidate)),
			order: index,
		})
	}
	sort.SliceStable(scoredCandidates, func(i, j int) bool {
		if scoredCandidates[i].score != scoredCandidates[j].score {
			return scoredCandidates[i].score < scoredCandidates[j].score
		}
		return scoredCandidates[i].order < scoredCandidates[j].order
	})
	if limit <= 0 || limit > len(scoredCandidates) {
		limit = len(scoredCandidates)
	}
	out := make([]string, 0, limit)
	for _, candidate := range scoredCandidates[:limit] {
		out = append(out, candidate.name)
	}
	return out
}

func normalizePattern(pattern string) string {
	var out strings.Builder
	for _, r := range strings.ToLower(pattern) {
		if !unicode.IsSpace(r) {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func editDistance(a, b string) int {
	ar, br := []rune(a), []rune(b)
	if len(ar) == 0 {
		return len(br)
	}
	previous := make([]int, len(br)+1)
	current := make([]int, len(br)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		current[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 0
			if ar[i-1] != br[j-1] {
				cost = 1
			}
			current[j] = min(previous[j]+1, current[j-1]+1, previous[j-1]+cost)
		}
		previous, current = current, previous
	}
	return previous[len(br)]
}
