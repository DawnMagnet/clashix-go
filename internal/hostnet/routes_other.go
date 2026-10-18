//go:build !linux && !darwin && !windows

package hostnet

func readSystemRoutes() ([]Route, []Rule, bool) {
	return nil, nil, false
}
