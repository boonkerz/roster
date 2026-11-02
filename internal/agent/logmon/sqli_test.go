package logmon

import (
	"testing"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

func TestSQLInjectionPatterns(t *testing.T) {
	src := shared.LogSource{Name: "web", Type: "file", Target: "/x", Format: "nginx_access"}
	line := func(req string, status string) string {
		return `203.0.113.5 - - [02/Nov/2026:08:00:00 +0000] "` + req + `" ` + status + ` 512 "-" "Mozilla"`
	}
	probe := []string{
		"POST /index.php?id=1%20union%20select%201,2,3 HTTP/1.1",
		"GET /search?q=1'+or+'1'='1 HTTP/1.1",
		"GET /item?id=1%27%20or%201%3d1-- HTTP/1.1",
		"GET /p?id=1/**/union/**/select/**/password/**/from/**/users HTTP/1.1",
		"GET /p?id=1;waitfor%20delay%20'0:0:5' HTTP/1.1",
		"GET /p?id=1%20and%20sleep(5) HTTP/1.1",
		"GET /p?id=(select*from(select(sleep(5)))a) HTTP/1.1",
		"GET /p?id=1'%20and%20extractvalue(1,concat(0x7e,version()))-- HTTP/1.1",
		"GET /login?user=admin'--&pw=x HTTP/1.1",
		"GET /p?id=1%20order%20by%201-- HTTP/1.1",
		"GET /?s=1%20union%20all%20select%20null,null HTTP/1.1",
		"GET /p?t=information_schema.tables HTTP/1.1",
	}
	for _, r := range probe {
		ev, ok := Parse(src, Unwrap(line(r, "200")+"\n"), time.Now())
		if !ok || ev.Kind != "probe" {
			t.Errorf("nicht erkannt: %s (%+v)", r, ev)
		}
	}
	legit := []string{
		"GET /blog/union-select-committee-2026 HTTP/1.1",
		"GET /shop?q=sleeping+bag&order=price HTTP/1.1",
		"GET /api/products?select=name,price&sort=-created HTTP/1.1",
		"GET /search?q=rock%20'n'%20roll HTTP/1.1",
		"GET /docs/version HTTP/1.1",
		"GET /article/how-to-concat-strings HTTP/1.1",
		"POST /checkout HTTP/1.1",
		"GET /p?id=1--2 HTTP/1.1",
	}
	for _, r := range legit {
		if ev, ok := Parse(src, Unwrap(line(r, "200")+"\n"), time.Now()); ok {
			t.Errorf("Fehlalarm: %s (%+v)", r, ev)
		}
	}
}
