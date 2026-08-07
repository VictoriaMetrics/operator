{{- define "gvDetails" -}}
{{- $gv := . }}

## {{ $gv.GroupVersionString }}

{{ $gv.Doc }}

{{- if $gv.Kinds  }}
### Resource Types
{{- range $gv.SortedKinds }}
{{- $t := $gv.TypeForKind . }}
{{- $version := $t.Package | splitList "/" | last }}
- [{{ $t.Name }}](#{{ lower (printf "%s-%s" $version $t.Name) }})
{{- end }}
{{- end }}
{{- range $gv.SortedTypes }}
{{- template "type" . }}
{{- end }}
{{- end -}}
