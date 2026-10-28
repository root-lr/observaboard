package runbook

import (
	"html"
	"strings"
	"testing"

	"github.com/root-lr/observaboard/assets"
)

func TestToHTML(t *testing.T) {
	body, title := ToHTML("# Titre <b>\n\nUn **gras** et `code <x>` [lien](https://a.example) [js](javascript:alert(1))\n\n- un\n- deux\n\n1. a\n2. b\n\n```promql\nrate(x{a=\"<b>\"}[5m])\n```\n")
	if title != "Titre <b>" {
		t.Errorf("title %q", title)
	}
	for _, want := range []string{
		"<h1>Titre &lt;b&gt;</h1>",
		"<strong>gras</strong>",
		"<code>code &lt;x&gt;</code>",
		`<a href="https://a.example">lien</a>`,
		`<a href="#">js</a>`,
		"<ul>\n<li>un</li>\n<li>deux</li>\n</ul>",
		"<ol>\n<li>a</li>\n<li>b</li>\n</ol>",
		`<pre><code class="lang-promql">rate(x{a=&#34;&lt;b&gt;&#34;}[5m])</code></pre>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
}

func TestRenderAllRunbooks(t *testing.T) {
	pages, err := RenderAll(assets.FS, "runbooks")
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) < 10 {
		t.Fatalf("only %d pages", len(pages))
	}
	for _, p := range pages {
		if p.Title == p.Name && p.Name != "index" {
			t.Errorf("%s has no H1 title", p.Name)
		}
		if p.Name == "index" {
			continue
		}
		for _, section := range []string{"Ce que signifie l'alerte", "Impact pour les utilisateurs", "Dashboards à ouvrir", "Requêtes utiles", "Causes probables", "Actions", "Vérifier le retour à la normale"} {
			if !strings.Contains(string(p.HTML), html.EscapeString(section)) {
				t.Errorf("%s: section %q missing", p.Name, section)
			}
		}
	}
}
