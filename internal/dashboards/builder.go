// Package dashboards generates the Grafana dashboards shipped with
// ObservaBoard. Dashboards are built in Go so that panels, queries and layout
// stay consistent, then written as classic dashboard JSON for file provisioning.
package dashboards

import (
	"encoding/json"
	"strings"
)

// Datasource uids provisioned on the central server.
const (
	DSPrometheus = "prometheus"
	DSLoki       = "loki"
	DSTempo      = "tempo"
)

// Dashboard is a dashboard under construction.
type Dashboard struct {
	UID, Title, Folder, Description string
	Tags                            []string
	Vars                            []Var
	Links                           []Link
	Refresh                         string
	panels                          []Panel
}

// Var is a template variable backed by a Prometheus label_values query.
type Var struct {
	Name, Label, Query string
	Multi              bool // multi-select with "All"
}

// Link is a dashboard link shown in the header.
type Link struct{ Title, URL string }

// Target is one query.
type Target struct {
	Expr    string // PromQL, LogQL or TraceQL
	Legend  string
	Instant bool
	Table   bool
}

// Panel is a dashboard panel. Width is in grid units (24 = full width).
type Panel struct {
	Type        string // timeseries, stat, table, logs, traceql, row, text
	Title       string
	Description string
	Width       int
	Height      int
	Unit        string
	Min, Max    *float64
	Decimals    *int
	Targets     []Target
	// Thresholds: list of {color, value}; first value is ignored (base).
	Thresholds []Threshold
	Stack      bool
	Bars       bool
	// Rename maps a table column ("Value #A") to a display name.
	Rename map[string]string
	// Hide lists table columns to hide.
	Hide []string
	// Content for text panels (markdown).
	Content string
	// Overrides adds raw fieldConfig overrides.
	Overrides []map[string]any
	// Links on table cells: field name -> URL.
	CellLinks map[string]string
	// Loki makes a timeseries panel query Loki (LogQL metric queries).
	Loki bool
}

// Threshold is a colour step.
type Threshold struct {
	Color string
	Value *float64
}

// F returns a pointer to a float, for Min/Max/Threshold values.
func F(v float64) *float64 { return &v }

// I returns a pointer to an int.
func I(v int) *int { return &v }

// Row starts a new titled section.
func (d *Dashboard) Row(title string) {
	d.panels = append(d.panels, Panel{Type: "row", Title: title, Width: 24, Height: 1})
}

// Add appends panels.
func (d *Dashboard) Add(p ...Panel) { d.panels = append(d.panels, p...) }

// JSON renders the dashboard.
func (d *Dashboard) JSON() ([]byte, error) {
	var panels []map[string]any
	x, y, rowH := 0, 0, 0
	for i, p := range d.panels {
		w, h := p.Width, p.Height
		if w == 0 {
			w = 12
		}
		if h == 0 {
			h = 8
		}
		if p.Type == "row" || x+w > 24 {
			if x > 0 {
				y += rowH
			}
			x, rowH = 0, 0
		}
		gp := map[string]any{"x": x, "y": y, "w": w, "h": h}
		panels = append(panels, d.panelJSON(i+1, p, gp))
		if p.Type == "row" {
			y += h
			continue
		}
		x += w
		if h > rowH {
			rowH = h
		}
	}
	var vars []map[string]any
	for _, v := range d.Vars {
		vars = append(vars, varJSON(v))
	}
	var links []map[string]any
	for _, l := range d.Links {
		links = append(links, map[string]any{
			"title": l.Title, "type": "link", "url": l.URL,
			"keepTime": true, "includeVars": false, "targetBlank": false, "icon": "dashboard",
		})
	}
	refresh := d.Refresh
	if refresh == "" {
		refresh = "1m"
	}
	out := map[string]any{
		"uid":           d.UID,
		"title":         d.Title,
		"description":   d.Description,
		"tags":          append([]string{"observaboard"}, d.Tags...),
		"timezone":      "browser",
		"editable":      true,
		"graphTooltip":  1,
		"refresh":       refresh,
		"schemaVersion": 41,
		"version":       1,
		"time":          map[string]any{"from": "now-3h", "to": "now"},
		"timepicker":    map[string]any{},
		"templating":    map[string]any{"list": nonNil(vars)},
		"annotations": map[string]any{"list": []map[string]any{{
			"builtIn": 1, "datasource": map[string]any{"type": "grafana", "uid": "-- Grafana --"},
			"enable": true, "hide": true, "iconColor": "rgba(0, 211, 255, 1)", "name": "Annotations & Alerts", "type": "dashboard",
		}}},
		"links":  nonNil(links),
		"panels": panels,
	}
	return json.MarshalIndent(out, "", "  ")
}

func nonNil(v []map[string]any) []map[string]any {
	if v == nil {
		return []map[string]any{}
	}
	return v
}

func varJSON(v Var) map[string]any {
	m := map[string]any{
		"name":       v.Name,
		"label":      v.Label,
		"type":       "query",
		"datasource": map[string]any{"type": "prometheus", "uid": DSPrometheus},
		// Plain string query: the long-standing format Grafana migrates itself,
		// independent of the query-editor schema of the Prometheus plugin version.
		"query":      v.Query,
		"definition": v.Query,
		"refresh":    2,
		"sort":       1,
		"hide":       0,
		"multi":      v.Multi,
		"includeAll": v.Multi,
		"current":    map[string]any{},
		"options":    []any{},
		"regex":      "",
	}
	if v.Multi {
		m["allValue"] = ".+"
	}
	return m
}

func dsFor(panelType string) map[string]any {
	switch panelType {
	case "logs":
		return map[string]any{"type": "loki", "uid": DSLoki}
	case "traceql":
		return map[string]any{"type": "tempo", "uid": DSTempo}
	default:
		return map[string]any{"type": "prometheus", "uid": DSPrometheus}
	}
}

func (d *Dashboard) panelJSON(id int, p Panel, gp map[string]any) map[string]any {
	if p.Type == "row" {
		return map[string]any{"id": id, "type": "row", "title": p.Title, "collapsed": false, "gridPos": gp, "panels": []any{}}
	}
	if p.Type == "text" {
		return map[string]any{
			"id": id, "type": "text", "title": p.Title, "gridPos": gp,
			"options": map[string]any{"mode": "markdown", "content": p.Content},
		}
	}
	ds := dsFor(p.Type)
	if p.Loki {
		ds = dsFor("logs")
	}
	var targets []map[string]any
	for i, t := range p.Targets {
		ref := string(rune('A' + i))
		tm := map[string]any{"refId": ref, "datasource": ds}
		switch {
		case p.Type == "logs" || p.Loki:
			tm["expr"] = t.Expr
			tm["queryType"] = "range"
			if t.Legend != "" {
				tm["legendFormat"] = t.Legend
			}
		case p.Type == "traceql":
			tm["queryType"] = "traceql"
			tm["query"] = t.Expr
			tm["limit"] = 20
			tm["tableType"] = "traces"
			tm["filters"] = []any{}
		default:
			tm["expr"] = t.Expr
			tm["legendFormat"] = t.Legend
			if t.Legend == "" {
				tm["legendFormat"] = "__auto"
			}
			tm["range"] = !t.Instant
			tm["instant"] = t.Instant
			if t.Table {
				tm["format"] = "table"
			}
		}
		targets = append(targets, tm)
	}

	defaults := map[string]any{}
	if p.Unit != "" {
		defaults["unit"] = p.Unit
	}
	if p.Min != nil {
		defaults["min"] = *p.Min
	}
	if p.Max != nil {
		defaults["max"] = *p.Max
	}
	if p.Decimals != nil {
		defaults["decimals"] = *p.Decimals
	}
	steps := []map[string]any{{"color": "green", "value": nil}}
	if len(p.Thresholds) > 0 {
		steps = nil
		for i, t := range p.Thresholds {
			var v any
			if i > 0 && t.Value != nil {
				v = *t.Value
			}
			steps = append(steps, map[string]any{"color": t.Color, "value": v})
		}
	}
	defaults["thresholds"] = map[string]any{"mode": "absolute", "steps": steps}
	defaults["color"] = map[string]any{"mode": "palette-classic"}

	panelType := p.Type
	options := map[string]any{}
	var transformations []map[string]any
	switch p.Type {
	case "timeseries":
		custom := map[string]any{
			"drawStyle": "line", "lineWidth": 1, "fillOpacity": 10, "showPoints": "never",
			"spanNulls": false, "gradientMode": "none", "lineInterpolation": "linear",
		}
		if p.Bars {
			custom["drawStyle"] = "bars"
			custom["fillOpacity"] = 80
		}
		if p.Stack {
			custom["stacking"] = map[string]any{"mode": "normal", "group": "A"}
			custom["fillOpacity"] = 40
		}
		defaults["custom"] = custom
		options["legend"] = map[string]any{"displayMode": "list", "placement": "bottom", "showLegend": true, "calcs": []any{}}
		options["tooltip"] = map[string]any{"mode": "multi", "sort": "desc"}
	case "stat":
		if len(p.Thresholds) > 0 {
			defaults["color"] = map[string]any{"mode": "thresholds"}
		}
		options = map[string]any{
			"reduceOptions": map[string]any{"calcs": []any{"lastNotNull"}, "fields": "", "values": false},
			"colorMode":     "value", "graphMode": "area", "justifyMode": "auto", "textMode": "auto",
			"orientation": "auto", "wideLayout": true, "showPercentChange": false,
		}
	case "table":
		options = map[string]any{"showHeader": true, "cellHeight": "sm", "footer": map[string]any{"show": false}}
		if len(p.Targets) > 1 {
			transformations = append(transformations, map[string]any{"id": "merge", "options": map[string]any{}})
		}
		exclude := map[string]any{"Time": true}
		for _, h := range p.Hide {
			exclude[h] = true
		}
		rename := map[string]any{}
		for k, v := range p.Rename {
			rename[k] = v
		}
		transformations = append(transformations, map[string]any{
			"id": "organize", "options": map[string]any{"excludeByName": exclude, "renameByName": rename, "indexByName": map[string]any{}},
		})
	case "logs":
		options = map[string]any{
			"showTime": true, "showLabels": false, "showCommonLabels": false, "wrapLogMessage": true,
			"prettifyLogMessage": false, "enableLogDetails": true, "dedupStrategy": "none", "sortOrder": "Descending",
		}
	case "traceql":
		panelType = "table"
		options = map[string]any{"showHeader": true, "cellHeight": "sm", "footer": map[string]any{"show": false}}
	}

	overrides := []map[string]any{}
	overrides = append(overrides, p.Overrides...)
	for field, url := range p.CellLinks {
		overrides = append(overrides, map[string]any{
			"matcher": map[string]any{"id": "byName", "options": field},
			"properties": []map[string]any{{
				"id": "links", "value": []map[string]any{{"title": "Ouvrir", "url": url}},
			}},
		})
	}

	m := map[string]any{
		"id":          id,
		"type":        panelType,
		"title":       p.Title,
		"description": p.Description,
		"datasource":  ds,
		"gridPos":     gp,
		"targets":     targets,
		"fieldConfig": map[string]any{"defaults": defaults, "overrides": overrides},
		"options":     options,
	}
	if transformations != nil {
		m["transformations"] = transformations
	}
	return m
}

// Sel builds a label selector from key=value pairs; values containing "$"
// variables of multi-select type use =~.
func Sel(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		k, v := pairs[i], pairs[i+1]
		op := "="
		if strings.HasPrefix(v, "~") {
			op, v = "=~", v[1:]
		} else if strings.HasPrefix(v, "!") {
			op, v = "!=", v[1:]
		}
		parts = append(parts, k+op+`"`+v+`"`)
	}
	return "{" + strings.Join(parts, ", ") + "}"
}
