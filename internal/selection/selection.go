// Package selection applies node selection specs to mihomo proxy groups.
package selection

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/metacubex/mihomo/adapter/outboundgroup"
	C "github.com/metacubex/mihomo/constant"
	mihomolog "github.com/metacubex/mihomo/log"
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
	raw   string
	re    *regexp.Regexp
	terms []string
}

// ParseSpecs parses selection specs. A spec without '=' applies to every
// select group containing a matching node; GROUP=PATTERN targets a group
// matched by GROUP.
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
	if len(m.terms) == 0 {
		return nil, fmt.Errorf("pattern is empty")
	}
	for _, term := range m.terms {
		if strings.HasPrefix(term, "type:") && strings.TrimSpace(term[len("type:"):]) == "" {
			return nil, fmt.Errorf("type: requires a protocol name")
		}
	}
	return m, nil
}

// regionAliases maps common region words to their flags. Two-letter aliases
// are treated as whole tokens so codes such as "us" do not match "plus".
type regionAlias struct {
	names []string
	flag  string
}

var regionAliases = []regionAlias{
	{names: []string{"日本", "japan", "jp"}, flag: "🇯🇵"},
	{names: []string{"香港", "hongkong", "hong kong", "hk"}, flag: "🇭🇰"},
	{names: []string{"台湾", "taiwan", "tw"}, flag: "🇹🇼"},
	{names: []string{"新加坡", "singapore", "sg"}, flag: "🇸🇬"},
	{names: []string{"美国", "usa", "united states", "us"}, flag: "🇺🇸"},
	{names: []string{"韩国", "korea", "south korea", "kr"}, flag: "🇰🇷"},
	{names: []string{"英国", "uk", "gb", "united kingdom", "great britain"}, flag: "🇬🇧"},
	{names: []string{"德国", "germany", "de"}, flag: "🇩🇪"},
	{names: []string{"加拿大", "canada", "ca"}, flag: "🇨🇦"},
}

func regionFor(word string) *regionAlias {
	for i := range regionAliases {
		for _, name := range regionAliases[i].names {
			if word == name {
				return &regionAliases[i]
			}
		}
	}
	return nil
}

func isShortTerm(term string) bool {
	return len(term) <= 3
}

func containsToken(candidate, token string) bool {
	for _, part := range strings.FieldsFunc(candidate, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if part == token {
			return true
		}
	}
	return false
}

func termMatches(term, candidate string) bool {
	if isShortTerm(term) {
		return containsToken(candidate, term)
	}
	return strings.Contains(candidate, term)
}

func wordMatches(word, candidate string) bool {
	if region := regionFor(word); region != nil {
		for _, alias := range region.names {
			if termMatches(alias, candidate) {
				return true
			}
		}
		return strings.Contains(candidate, region.flag)
	}
	return termMatches(word, candidate)
}

// protocolWords maps a bare query word to the mihomo adapter type it selects.
// Words in this table are treated as type matches, not name substrings, so
// "trojan" selects Trojan nodes instead of nodes whose names contain "TR".
var protocolWords = map[string]string{
	"ss":           "ss",
	"shadowsocks":  "ss",
	"ssr":          "ssr",
	"shadowsocksr": "ssr",
	"socks":        "socks5",
	"socks5":       "socks5",
	"hy":           "hysteria",
	"hysteria":     "hysteria",
	"hy2":          "hysteria2",
	"hysteria2":    "hysteria2",
	"wg":           "wireguard",
	"wireguard":    "wireguard",
	"anytls":       "anytls",
	"vless":        "vless",
	"vmess":        "vmess",
	"trojan":       "trojan",
	"tuic":         "tuic",
	"mieru":        "mieru",
	"snell":        "snell",
	"http":         "http",
}

func protocolWord(word string) (string, bool) {
	canonical, ok := protocolWords[strings.ToLower(strings.TrimSpace(word))]
	return canonical, ok
}

func typeMatches(candidateType, protocol string) bool {
	candidateType = strings.ToLower(strings.TrimSpace(candidateType))
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	if candidateType == "" || protocol == "" {
		return false
	}
	if canonical, ok := protocolWord(protocol); ok {
		protocol = canonical
	}
	if candidateType == protocol {
		return true
	}
	for word, canonical := range protocolWords {
		if canonical == protocol && candidateType == word {
			return true
		}
	}
	return false
}

func (m *matcher) matches(candidate string) bool {
	return m.matchesCandidate(candidate, "")
}

func (m *matcher) matchesCandidate(name, kind string) bool {
	if m.re != nil {
		return m.re.MatchString(name)
	}
	lower := strings.ToLower(name)
	for _, term := range m.terms {
		if !m.termMatches(term, lower, kind) {
			return false
		}
	}
	return len(m.terms) > 0
}

func (m *matcher) termMatches(term, candidate, kind string) bool {
	if strings.HasPrefix(term, "type:") {
		return typeMatches(kind, term[len("type:"):])
	}
	if kind != "" {
		if canonical, ok := protocolWord(term); ok {
			return typeMatches(kind, canonical)
		}
	}
	return wordMatches(term, candidate)
}

// match applies the matcher to a candidate list in order.
func (m *matcher) match(candidates []string) (string, bool) {
	for _, candidate := range candidates {
		if m.matches(candidate) {
			return candidate, true
		}
	}
	return "", false
}

func (m *matcher) matchProxy(members []C.Proxy) (string, bool) {
	for _, member := range members {
		if isPlaceholder(member) {
			continue
		}
		if m.matchesCandidate(member.Name(), member.Adapter().Type().String()) {
			return member.Name(), true
		}
	}
	return "", false
}

// matchPlaceholders returns the placeholder members that match m. Explicit
// selections use this to warn before falling back to a real node.
func (m *matcher) matchPlaceholders(members []C.Proxy) []string {
	var names []string
	for _, member := range members {
		if !isPlaceholder(member) {
			continue
		}
		if m.matchesCandidate(member.Name(), member.Adapter().Type().String()) {
			names = append(names, member.Name())
		}
	}
	return names
}

// bestRealNode ranks the group's real nodes with the same relevance scoring as
// selection errors and returns the first one.
func (m *matcher) bestRealNode(members []C.Proxy) (string, bool) {
	candidates := proxyMemberCandidates(members)
	if len(candidates) == 0 {
		return "", false
	}
	return closest(m, candidates, 1)[0], true
}

type placeholderFallback struct {
	group        string
	placeholders []string
	selected     string
}

func warnPlaceholderFallbacks(pattern string, fallbacks []placeholderFallback) {
	if len(fallbacks) == 0 {
		return
	}
	groups := make([]string, 0, len(fallbacks))
	for _, fallback := range fallbacks {
		groups = append(groups, fmt.Sprintf("%s (%s) -> %s", fallback.group, strings.Join(fallback.placeholders, ", "), fallback.selected))
	}
	mihomolog.Warnln("node selection %q matched only placeholder nodes; falling back to the best-ranked real nodes in %s", pattern, strings.Join(groups, "; "))
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
	return "", fmt.Errorf("no select proxy group matches %q; closest groups: %s", m.raw, formatClosest(m, nameCandidates(candidates), 5))
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
		if err := applyBarePattern(cfg, spec, groups, proxies, primary, targeted); err != nil {
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

func applyBarePattern(cfg map[string]any, spec Spec, groups []groupInfo, proxies map[string]C.Proxy, primary string, targeted map[string]bool) error {
	if len(groups) == 0 {
		message := fmt.Sprintf("no select proxy group found for node selection %q", spec.Pattern)
		message += ungroupedProxyNote(cfg, groups, spec.patternMatcher)
		return errors.New(message)
	}

	type plan struct {
		group    string
		selected string
	}
	var plans []plan
	var fallbacks []placeholderFallback
	matched := 0
	fallbackMatched := 0
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
		if selected, ok := spec.patternMatcher.matchProxy(members); ok {
			plans = append(plans, plan{group: info.name, selected: selected})
			matched++
			continue
		}
		if placeholders := spec.patternMatcher.matchPlaceholders(members); len(placeholders) > 0 {
			if selected, ok := spec.patternMatcher.bestRealNode(members); ok {
				plans = append(plans, plan{group: info.name, selected: selected})
				fallbackMatched++
				fallbacks = append(fallbacks, placeholderFallback{group: info.name, placeholders: placeholders, selected: selected})
			}
			continue
		}
		if info.name != primary && primary != "" && containsMember(members, primary) {
			plans = append(plans, plan{group: info.name, selected: primary})
		}
	}

	if matched == 0 && fallbackMatched == 0 {
		candidates := proxyCandidates(groups, proxies, "")
		message := fmt.Sprintf("no proxy node in any select group matches %q; the selection applies to all select groups containing a matching node; closest candidates: %s", spec.Pattern, formatClosest(spec.patternMatcher, candidates, 5))
		message += ungroupedProxyNote(cfg, groups, spec.patternMatcher)
		return errors.New(message)
	}
	warnPlaceholderFallbacks(spec.Pattern, fallbacks)

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

func ungroupedProxyNote(cfg map[string]any, groups []groupInfo, m *matcher) string {
	members := make(map[string]struct{})
	for _, group := range groups {
		if group.kind != "select" {
			continue
		}
		for _, name := range group.members {
			members[name] = struct{}{}
		}
	}
	var matches []string
	for _, name := range configProxyNames(cfg) {
		if _, grouped := members[name]; grouped {
			continue
		}
		if m.matches(name) {
			matches = append(matches, name)
		}
	}
	if len(matches) == 0 {
		return ""
	}
	return fmt.Sprintf("; matches %d proxies that are not members of any select group: %s", len(matches), strings.Join(matches, ", "))
}

func configProxyNames(cfg map[string]any) []string {
	items := anySlice(cfg["proxies"])
	names := make([]string, 0, len(items))
	for _, item := range items {
		mapping, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if name, ok := mapping["name"].(string); ok && name != "" {
			names = append(names, name)
		}
	}
	return names
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
	selected, ok := spec.patternMatcher.matchProxy(members)
	if !ok {
		placeholders := spec.patternMatcher.matchPlaceholders(members)
		if len(placeholders) == 0 {
			return fmt.Errorf("no proxy node in group %q matches %q; closest candidates: %s", groupName, spec.Pattern, formatClosest(spec.patternMatcher, proxyMemberCandidates(members), 5))
		}
		selected, ok = spec.patternMatcher.bestRealNode(members)
		if !ok {
			return fmt.Errorf("no proxy node in group %q matches %q; closest candidates: %s", groupName, spec.Pattern, formatClosest(spec.patternMatcher, proxyMemberCandidates(members), 5))
		}
		warnPlaceholderFallbacks(spec.Pattern, []placeholderFallback{{group: groupName, placeholders: placeholders, selected: selected}})
	}
	if err := setSelected(proxy, selected); err != nil {
		return fmt.Errorf("select %q in group %q: %w", selected, groupName, err)
	}
	targeted[groupName] = true
	return nil
}

func containsMember(members []C.Proxy, name string) bool {
	for _, member := range members {
		if member.Name() == name {
			return true
		}
	}
	return false
}

func proxyMemberCandidates(members []C.Proxy) []nodeCandidate {
	seen := make(map[string]struct{})
	candidates := make([]nodeCandidate, 0, len(members))
	for _, member := range members {
		if !isNodeCandidate(member) {
			continue
		}
		name := member.Name()
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		candidates = append(candidates, nodeCandidate{name: name, kind: member.Adapter().Type().String()})
	}
	return candidates
}

func isNodeCandidate(proxy C.Proxy) bool {
	if _, group := proxy.Adapter().(outboundgroup.ProxyGroup); group {
		return false
	}
	switch proxy.Adapter().Type() {
	case C.Direct, C.Reject, C.RejectDrop, C.Compatible, C.Pass, C.PassRule, C.Rematch, C.Dns:
		return false
	}
	return !isPlaceholder(proxy)
}

func proxyCandidates(groups []groupInfo, proxies map[string]C.Proxy, onlyGroup string) []nodeCandidate {
	seen := make(map[string]struct{})
	var candidates []nodeCandidate
	for _, info := range groups {
		if info.kind != "select" || (onlyGroup != "" && info.name != onlyGroup) {
			continue
		}
		proxy, ok := proxies[info.name]
		if !ok {
			continue
		}
		for _, candidate := range proxyMemberCandidates(groupMembers(proxy)) {
			if _, exists := seen[candidate.name]; exists {
				continue
			}
			seen[candidate.name] = struct{}{}
			candidates = append(candidates, candidate)
		}
	}
	return candidates
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
	if isPlaceholderServer(address) {
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

// isPlaceholderServer reports whether address is empty or uses a server that
// only exists as a subscription info/placeholder row.
func isPlaceholderServer(address string) bool {
	host := strings.TrimSpace(address)
	if host == "" {
		return true
	}
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" {
		return true
	}
	switch host {
	case "10.255.255.255", "0.0.0.0":
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

type nodeCandidate struct {
	name string
	kind string
}

func nameCandidates(names []string) []nodeCandidate {
	out := make([]nodeCandidate, 0, len(names))
	for _, name := range names {
		out = append(out, nodeCandidate{name: name})
	}
	return out
}

func formatClosest(m *matcher, candidates []nodeCandidate, limit int) string {
	names := closest(m, candidates, limit)
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

// closest ranks candidates by protocol/type similarity, region alias matches,
// shared tokens, normalized longest common substring, normalized shared
// characters, and finally the original group/member order.
func closest(m *matcher, candidates []nodeCandidate, limit int) []string {
	type scored struct {
		name  string
		score relevanceScore
		order int
	}
	scoredCandidates := make([]scored, 0, len(candidates))
	for index, candidate := range candidates {
		scoredCandidates = append(scoredCandidates, scored{
			name:  candidate.name,
			score: m.relevance(candidate.name, candidate.kind),
			order: index,
		})
	}
	sort.SliceStable(scoredCandidates, func(i, j int) bool {
		left, right := scoredCandidates[i].score, scoredCandidates[j].score
		if left.protocol != right.protocol {
			return left.protocol > right.protocol
		}
		if left.region != right.region {
			return left.region > right.region
		}
		if left.tokens != right.tokens {
			return left.tokens > right.tokens
		}
		if left.lcs != right.lcs {
			return left.lcs > right.lcs
		}
		if left.chars != right.chars {
			return left.chars > right.chars
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

type relevanceScore struct {
	protocol int
	region   int
	tokens   int
	lcs      float64
	chars    float64
}

func (m *matcher) relevance(name, kind string) relevanceScore {
	if m.re != nil {
		if m.re.MatchString(name) {
			return relevanceScore{tokens: 1}
		}
		return relevanceScore{}
	}
	lower := strings.ToLower(name)
	score := relevanceScore{
		protocol: m.protocolMatches(kind),
		region:   m.regionMatches(lower),
		tokens:   m.sharedTokens(lower),
	}
	pattern := m.textPattern()
	if pattern != "" && lower != "" {
		patternRunes := []rune(pattern)
		nameRunes := []rune(lower)
		score.lcs = float64(longestCommonSubstring(patternRunes, nameRunes)) / float64(maxInt(len(patternRunes), len(nameRunes)))
		score.chars = sharedCharacterRatio(patternRunes, nameRunes)
	}
	return score
}

func (m *matcher) protocolMatches(kind string) int {
	if m.re != nil {
		return 0
	}
	count := 0
	for _, term := range m.terms {
		protocol := term
		if strings.HasPrefix(term, "type:") {
			protocol = strings.TrimSpace(term[len("type:"):])
		} else if canonical, ok := protocolWord(term); ok {
			protocol = canonical
		} else {
			continue
		}
		if typeMatches(kind, protocol) {
			count++
		}
	}
	return count
}

func (m *matcher) regionMatches(candidate string) int {
	if m.re != nil {
		return 0
	}
	count := 0
	for _, term := range m.terms {
		region := regionFor(term)
		if region == nil {
			continue
		}
		matched := false
		for _, alias := range region.names {
			if termMatches(alias, candidate) {
				matched = true
				break
			}
		}
		if !matched {
			matched = strings.Contains(candidate, region.flag)
		}
		if matched {
			count++
		}
	}
	return count
}

func (m *matcher) sharedTokens(candidate string) int {
	if m.re != nil {
		return 0
	}
	tokens := make(map[string]struct{})
	for _, token := range strings.FieldsFunc(candidate, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		tokens[token] = struct{}{}
	}
	count := 0
	for _, term := range m.terms {
		if strings.HasPrefix(term, "type:") {
			continue
		}
		if _, ok := protocolWord(term); ok {
			continue
		}
		if _, ok := tokens[term]; ok {
			count++
			continue
		}
		if region := regionFor(term); region != nil {
			if _, ok := tokens[region.flag]; ok {
				count++
				continue
			}
			for _, alias := range region.names {
				if _, ok := tokens[alias]; ok {
					count++
					break
				}
			}
			continue
		}
		if len(term) > 2 && termMatches(term, candidate) {
			count++
		}
	}
	return count
}

func (m *matcher) textPattern() string {
	if m.re != nil {
		return m.raw
	}
	terms := make([]string, 0, len(m.terms))
	for _, term := range m.terms {
		if strings.HasPrefix(term, "type:") || isShortTerm(term) {
			continue
		}
		if _, ok := protocolWord(term); ok {
			continue
		}
		terms = append(terms, term)
	}
	return strings.Join(terms, " ")
}

func longestCommonSubstring(left, right []rune) int {
	best := 0
	previous := make([]int, len(right)+1)
	for i := range left {
		current := make([]int, len(right)+1)
		for j := range right {
			if left[i] == right[j] {
				current[j+1] = previous[j] + 1
				if current[j+1] > best {
					best = current[j+1]
				}
			}
		}
		previous = current
	}
	return best
}

func sharedCharacterRatio(left, right []rune) float64 {
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	counts := make(map[rune]int, len(left))
	for _, char := range left {
		counts[char]++
	}
	common := 0
	for _, char := range right {
		if counts[char] > 0 {
			counts[char]--
			common++
		}
	}
	return float64(common) / float64(maxInt(len(left), len(right)))
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
