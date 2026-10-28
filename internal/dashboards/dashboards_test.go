package dashboards_test

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/root-lr/observaboard/internal/central"
	"github.com/root-lr/observaboard/internal/dashboards"
	"github.com/root-lr/observaboard/internal/presets"
)

type panel struct {
	ID         int    `json:"id"`
	Type       string `json:"type"`
	Title      string `json:"title"`
	Datasource struct {
		UID string `json:"uid"`
	} `json:"datasource"`
	GridPos struct{ X, Y, W, H int } `json:"gridPos"`
	Targets []map[string]any         `json:"targets"`
}

func TestDashboardsAreWellFormed(t *testing.T) {
	rules, err := central.RenderRules("https://x")
	if err != nil {
		t.Fatal(err)
	}
	recorded := regexp.MustCompile(`record: (observa:[a-z0-9_:]+)`).FindAllStringSubmatch(string(rules), -1)
	have := map[string]bool{}
	for _, m := range recorded {
		have[m[1]] = true
	}
	uids := map[string]bool{}
	for _, d := range dashboards.All() {
		if uids[d.UID] {
			t.Errorf("duplicate uid %s", d.UID)
		}
		uids[d.UID] = true
		data, err := d.JSON()
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			UID        string  `json:"uid"`
			Panels     []panel `json:"panels"`
			Templating struct {
				List []struct{ Name string } `json:"list"`
			} `json:"templating"`
		}
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatalf("%s: %v", d.UID, err)
		}
		vars := map[string]bool{}
		for _, v := range doc.Templating.List {
			vars[v.Name] = true
		}
		ids := map[int]bool{}
		for _, p := range doc.Panels {
			if ids[p.ID] {
				t.Errorf("%s: duplicate panel id %d", d.UID, p.ID)
			}
			ids[p.ID] = true
			if p.GridPos.X+p.GridPos.W > 24 {
				t.Errorf("%s/%s: panel overflows the grid", d.UID, p.Title)
			}
			if p.Type == "row" || p.Type == "text" {
				continue
			}
			switch p.Datasource.UID {
			case dashboards.DSPrometheus, dashboards.DSLoki, dashboards.DSTempo:
			default:
				t.Errorf("%s/%s: unknown datasource %q", d.UID, p.Title, p.Datasource.UID)
			}
			if len(p.Targets) == 0 {
				t.Errorf("%s/%s: no query", d.UID, p.Title)
			}
			for _, tg := range p.Targets {
				q, _ := tg["expr"].(string)
				if q == "" {
					q, _ = tg["query"].(string)
				}
				for _, m := range regexp.MustCompile(`observa:[a-z0-9_:]+`).FindAllString(q, -1) {
					if !have[m] {
						t.Errorf("%s/%s: uses unrecorded series %s", d.UID, p.Title, m)
					}
				}
				for _, v := range regexp.MustCompile(`\$([a-z]+)`).FindAllStringSubmatch(q, -1) {
					if !vars[v[1]] {
						t.Errorf("%s/%s: uses undefined variable $%s", d.UID, p.Title, v[1])
					}
				}
				if strings.Count(q, "(") != strings.Count(q, ")") || strings.Count(q, "{") != strings.Count(q, "}") {
					t.Errorf("%s/%s: unbalanced query %q", d.UID, p.Title, q)
				}
			}
		}
	}
	for _, p := range presets.All() {
		if !uids[p.Dashboard] {
			t.Errorf("preset %s points to missing dashboard %s", p.Name, p.Dashboard)
		}
	}
}
