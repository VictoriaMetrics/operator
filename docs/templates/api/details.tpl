{{- define "gvDetails" -}}
{{- $gv := . -}}

## {{ $gv.GroupVersionString }}
{{- if $gv.Doc }}

{{ trim $gv.Doc }}
{{- end }}
{{- if $gv.Kinds }}

### Resource Types
{{- if $gv.SortedKinds}}
{{- range $gv.SortedKinds }}
- {{ $gv.TypeForKind . | markdownRenderTypeLink }}
{{- end }}
{{- end }}

{{- end }}
{{- if $gv.SortedTypes }}
{{ range $gv.SortedTypes }}
{{ template "type" . }}
{{- end }}

{{- end }}
{{- end -}}
