{{- define "type" -}}
{{- $type := . -}}
{{- if markdownShouldRenderType $type -}}

#### {{ $type.Name }}

{{ if $type.IsAlias }}_Underlying type:_ _{{ markdownRenderTypeLink $type.UnderlyingType }}_{{ end }}

{{ $type.Doc }}

{{ if $type.Validation -}}
_Validation:_
{{- range $type.Validation }}
- {{ . }}
{{- end }}
{{- end }}

{{ if $type.References -}}
_Appears in:_
{{- range $type.SortedReferences }}
- {{ markdownRenderTypeLink . }}
{{- end }}
{{- end }}

{{ if $type.Members -}}
| Field | Description | Scheme | Required |
| --- | --- | --- | --- |
{{ if $type.GVK -}}
| `apiVersion` _string_ | `{{ $type.GVK.Group }}/{{ $type.GVK.Version }}` | | |
| `kind` _string_ | `{{ $type.GVK.Kind }}` | | |
{{ end -}}
{{- $members := default dict -}}
{{- range $member := $type.Members -}}
{{- $_ := set $members $member.Name $member }}
{{- end -}}
{{- $memberKeys := (keys $members | sortAlpha) -}}
{{ range $memberKeys -}}
{{- $member := index $members . -}}
| `{{ $member.Name }}` | {{ template "type_members" $member }} | _{{ markdownRenderType $member.Type }}_ | {{ not $member.Markers.optional }} |
{{ end -}}

{{- end -}}
{{- end -}}
{{- end -}}
