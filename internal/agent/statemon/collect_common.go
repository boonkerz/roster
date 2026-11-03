package statemon

import (
	"context"
	"fmt"

	"github.com/boonkerz/roster/internal/agent/collect"
)

// collectPorts: lauschende Ports ohne PID (der wechselt bei jedem Neustart).
func collectPorts(ctx context.Context) []string {
	var out []string
	for _, p := range collect.ListenPorts(ctx) {
		out = append(out, fmt.Sprintf("%s %s:%d %s", p.Proto, p.Address, p.Port, p.Process))
	}
	return out
}
