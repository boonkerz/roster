package logmon

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

func TestLiveScan(t *testing.T) {
	path := os.Getenv("ROSTER_LIVE_LOG")
	if path == "" {
		t.Skip()
	}
	f, _ := os.Open(path)
	defer f.Close()
	src := shared.LogSource{Name: "proxy", Type: "docker", Target: "nginx-proxy", Format: "nginx_access"}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	kinds := map[string]int{}
	ips := map[string]int{}
	var miss []string
	n := 0
	for sc.Scan() {
		n++
		l := Unwrap(sc.Text() + "\n")
		ev, ok := Parse(src, l, time.Now())
		if ok {
			kinds[ev.Kind]++
			ips[ev.SourceIP]++
			continue
		}
		m := reAccess.FindStringSubmatch(l.Text)
		if m == nil {
			miss = append(miss, "UNPARSED: "+l.Text)
			continue
		}
		if st := m[reAccess.SubexpIndex("status")]; st != "200" && st != "301" && st != "302" && st != "304" && st != "206" {
			miss = append(miss, l.Text)
		}
	}
	fmt.Println("lines", n, "kinds", kinds)
	type kv struct {
		k string
		v int
	}
	var top []kv
	for k, v := range ips {
		top = append(top, kv{k, v})
	}
	sort.Slice(top, func(i, j int) bool { return top[i].v > top[j].v })
	if len(top) > 8 {
		top = top[:8]
	}
	fmt.Println("top", top)
	if len(miss) > 40 {
		miss = miss[len(miss)-40:]
	}
	for _, m := range miss {
		if len(m) > 230 {
			m = m[:230]
		}
		fmt.Println("MISS", m)
	}
}
