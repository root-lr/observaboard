// Package runbook turns the Markdown runbooks shipped in assets/runbooks into
// static HTML pages served by Caddy under /runbooks/. It implements the small
// Markdown subset the runbooks use: headings, paragraphs, lists, fenced code,
// inline code, bold, italics and links.
package runbook

import (
	"fmt"
	"html"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
)

// Page is a rendered runbook.
type Page struct {
	Name  string // file name without extension, e.g. observa-high-error-rate
	Title string
	HTML  []byte
}

var (
	codeSpanRE = regexp.MustCompile("`([^`]+)`")
	boldRE     = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	italicRE   = regexp.MustCompile(`(^|[^*])\*([^*\s][^*]*)\*`)
	linkRE     = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)`)
	olRE       = regexp.MustCompile(`^\d+\.\s+`)
)

// inline renders inline Markdown in an already-trimmed line.
func inline(s string) string {
	// Protect code spans first so their content is not formatted.
	var codes []string
	s = codeSpanRE.ReplaceAllStringFunc(s, func(m string) string {
		codes = append(codes, m[1:len(m)-1])
		return fmt.Sprintf("\x00%d\x00", len(codes)-1)
	})
	s = html.EscapeString(s)
	s = linkRE.ReplaceAllStringFunc(s, func(m string) string {
		p := linkRE.FindStringSubmatch(m)
		href := p[2]
		if !strings.HasPrefix(href, "http://") && !strings.HasPrefix(href, "https://") && !strings.HasPrefix(href, "/") && !strings.HasPrefix(href, "#") {
			href = "#"
		}
		return `<a href="` + href + `">` + p[1] + `</a>`
	})
	s = boldRE.ReplaceAllString(s, "<strong>$1</strong>")
	s = italicRE.ReplaceAllString(s, "$1<em>$2</em>")
	for i, c := range codes {
		s = strings.Replace(s, fmt.Sprintf("\x00%d\x00", i), "<code>"+html.EscapeString(c)+"</code>", 1)
	}
	return s
}

// ToHTML converts Markdown to an HTML fragment and returns the first H1 title.
func ToHTML(md string) (body, title string) {
	var (
		b       strings.Builder
		para    []string
		list    string // "ul", "ol" or ""
		inCode  bool
		codeBuf strings.Builder
		codeLng string
	)
	flushPara := func() {
		if len(para) > 0 {
			b.WriteString("<p>" + inline(strings.Join(para, " ")) + "</p>\n")
			para = nil
		}
	}
	closeList := func() {
		if list != "" {
			b.WriteString("</" + list + ">\n")
			list = ""
		}
	}
	for _, raw := range strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n") {
		line := strings.TrimRight(raw, " \t")
		if inCode {
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				cls := ""
				if codeLng != "" {
					cls = ` class="lang-` + html.EscapeString(codeLng) + `"`
				}
				b.WriteString("<pre><code" + cls + ">" + html.EscapeString(strings.TrimRight(codeBuf.String(), "\n")) + "</code></pre>\n")
				codeBuf.Reset()
				inCode = false
				continue
			}
			codeBuf.WriteString(raw + "\n")
			continue
		}
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "```"):
			flushPara()
			closeList()
			inCode = true
			codeLng = strings.TrimSpace(strings.TrimPrefix(t, "```"))
		case t == "":
			flushPara()
			closeList()
		case strings.HasPrefix(t, "#"):
			flushPara()
			closeList()
			level := len(t) - len(strings.TrimLeft(t, "#"))
			if level > 4 {
				level = 4
			}
			text := strings.TrimSpace(t[level:])
			if level == 1 && title == "" {
				title = text
			}
			fmt.Fprintf(&b, "<h%d>%s</h%d>\n", level, inline(text), level)
		case strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* "):
			flushPara()
			if list != "ul" {
				closeList()
				b.WriteString("<ul>\n")
				list = "ul"
			}
			b.WriteString("<li>" + inline(strings.TrimSpace(t[2:])) + "</li>\n")
		case olRE.MatchString(t):
			flushPara()
			if list != "ol" {
				closeList()
				b.WriteString("<ol>\n")
				list = "ol"
			}
			b.WriteString("<li>" + inline(olRE.ReplaceAllString(t, "")) + "</li>\n")
		default:
			if list != "" {
				closeList()
			}
			para = append(para, t)
		}
	}
	if inCode {
		b.WriteString("<pre><code>" + html.EscapeString(codeBuf.String()) + "</code></pre>\n")
	}
	flushPara()
	closeList()
	return b.String(), title
}

const pageTmpl = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>%s · ObservaBoard runbooks</title>
<style>
:root{--bg:#fbfbfa;--fg:#1d1f21;--muted:#5d6166;--line:#e3e3df;--code:#f1f1ee;--accent:#2557d6}
@media (prefers-color-scheme:dark){:root{--bg:#141517;--fg:#e6e6e3;--muted:#a0a3a8;--line:#2b2d31;--code:#1e2024;--accent:#7aa2ff}}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--fg);font:16px/1.6 system-ui,-apple-system,"Segoe UI",Roboto,sans-serif}
main{max-width:760px;margin:0 auto;padding:32px 16px 64px}
nav{font-size:14px;margin-bottom:24px}
a{color:var(--accent)}
h1{font-size:26px;line-height:1.3;margin:0 0 12px}
h2{font-size:19px;margin:32px 0 8px;padding-top:16px;border-top:1px solid var(--line)}
h3{font-size:16px;margin:24px 0 8px}
p,li{color:var(--fg)}
code{font:13.5px/1.5 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;background:var(--code);padding:1px 5px;border-radius:4px}
pre{background:var(--code);border:1px solid var(--line);border-radius:8px;padding:12px 14px;overflow-x:auto}
pre code{background:none;padding:0}
ul,ol{padding-left:22px}
.list li{margin:6px 0}
.muted{color:var(--muted)}
</style>
</head>
<body>
<main>
<nav><a href="/runbooks/">All runbooks</a> · <a href="/">Grafana</a></nav>
%s
</main>
</body>
</html>
`

// RenderAll converts every *.md file of dir in fsys and adds an index page.
func RenderAll(fsys fs.FS, dir string) ([]Page, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, err
	}
	var pages []Page
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		body, title := ToHTML(string(data))
		name := strings.TrimSuffix(e.Name(), ".md")
		if title == "" {
			title = name
		}
		pages = append(pages, Page{Name: name, Title: title, HTML: []byte(fmt.Sprintf(pageTmpl, html.EscapeString(title), body))})
	}
	sort.Slice(pages, func(i, j int) bool { return pages[i].Name < pages[j].Name })
	var idx strings.Builder
	idx.WriteString("<h1>ObservaBoard runbooks</h1>\n<p class=\"muted\">One procedure per alert: what it means, where to look, what to do.</p>\n<ul class=\"list\">\n")
	for _, p := range pages {
		fmt.Fprintf(&idx, "<li><a href=\"/runbooks/%s\">%s</a></li>\n", p.Name, html.EscapeString(p.Title))
	}
	idx.WriteString("</ul>\n")
	pages = append(pages, Page{Name: "index", Title: "Runbooks", HTML: []byte(fmt.Sprintf(pageTmpl, "Runbooks", idx.String()))})
	return pages, nil
}
