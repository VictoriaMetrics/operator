package config

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strings"
	"text/tabwriter"
	"text/template"

	"github.com/caarlos0/env/v11"
)

var formats = map[string]string{
	"table": `KEY	DEFAULT	REQUIRED	DESCRIPTION
{{- range $idx, $item := .Params }}
{{ $item.Key }}	{{ $item.DefaultValue }}	{{ $item.Required }}	{{ index $.Descriptions $idx }}
{{- end }}
`,
	"list": `
{{- range $idx, $item := .Params -}}
{{ .Key }}
	[default]	{{ $item.DefaultValue }}
	[required]	{{ $item.Required }}
        [description]	{{ index $.Descriptions $idx }}
{{ end -}}
`,
	"markdown": `
| variable name | variable default value | variable required | variable description |
| --- | --- | --- | --- |
{{- range $idx, $item := .Params }}
| {{ $item.Key }} | {{ if eq (len $item.DefaultValue) 0 -}}-{{- else -}}{{ $item.DefaultValue }}{{- end }} | {{ $item.Required }} | {{ index $.Descriptions $idx }} |
{{- end }}
`,

	"json": `
{{- $last := (len (slice . 1)) -}}
{
	{{- range $idx, $item := .Params }}
	"{{ $item.Key }}": "{{ $item.DefaultValue }}"{{ if lt $idx $last }},{{ end }}
	{{- end }}
}
`,
	"yaml": `
{{- range $idx, $item := .Params }}
{{ $item.Key }}: '{{ $item.DefaultValue }}'
{{- end }}
`}

// PrintDefaults prints default values for all config variables.
// format can be one of: table, list, json, yaml, markdown.
func (boc *BaseOperatorConf) PrintDefaults(format string) error {

	tpl, ok := formats[format]
	if !ok {
		return fmt.Errorf("unknown print format %q", format)
	}
	params, err := env.GetFieldParamsWithOptions(boc, getEnvOpts())
	if err != nil {
		return fmt.Errorf("failed to get field params: %w", err)
	}
	tmpl, err := template.New("env").Parse(tpl)
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}
	w := tabwriter.NewWriter(os.Stdout, 1, 0, 4, ' ', 0)
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "internal/config/config.go", nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}
	var descriptions []string
	for _, v := range f.Decls {
		g, ok := v.(*ast.GenDecl)
		if !ok {
			continue
		}
		if g.Doc == nil {
			continue
		}
		needGen := false
		for _, doc := range g.Doc.List {
			needGen = needGen || strings.HasPrefix(doc.Text, "//genvars:true")
		}
		if needGen {
			for _, d := range g.Specs {
				spec, ok := d.(*ast.TypeSpec)
				if !ok {
					continue
				}
				_, ok = spec.Type.(*ast.StructType)
				if !ok {
					continue
				}
				descriptions = append(descriptions, getFieldsDescriptions(spec.Type.(*ast.StructType))...)
			}
		}

	}
	err = tmpl.Execute(w, map[string]interface{}{
		"Params":       params,
		"Descriptions": descriptions,
	})
	if err != nil {
		return fmt.Errorf("failed to execute template: %w", err)
	}
	w.Flush()
	return nil
}

func getFieldsDescriptions(currStruct *ast.StructType) []string {
	var descriptions []string
	for _, field := range currStruct.Fields.List {
		switch field.Type.(type) {
		case *ast.StructType:
			descriptions = append(descriptions, getFieldsDescriptions(field.Type.(*ast.StructType))...)
		case *ast.Ident, *ast.SelectorExpr, *ast.ArrayType, *ast.MapType:
			if field.Tag != nil {
				tag := reflect.StructTag(field.Tag.Value[1 : len(field.Tag.Value)-1])
				if tag.Get("env") == "-" {
					break
				}
			}
			if field.Doc != nil {
				var fieldComments strings.Builder
				for i, comment := range field.Doc.List {
					commentValue := comment.Text
					commentValue = strings.TrimLeft(commentValue, "/")
					if i == 0 {
						commentValue = strings.TrimLeft(commentValue, " ")
					}
					if strings.Contains(commentValue, "TODO") || strings.HasPrefix(commentValue, "+") {
						continue
					}
					fieldComments.WriteString(commentValue)
				}
				descriptions = append(descriptions, fieldComments.String())
			} else {
				descriptions = append(descriptions, "")
			}
		}
	}
	return descriptions
}
