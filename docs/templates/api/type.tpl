{{- define "deprecated" -}}
  {{- $member := .member }}
  {{- $type := .type }}
  {{- $deprecated := default dict }}
  {{- range $member.Markers.deprecated }}
    {{- $deprecated = mergeOverwrite $deprecated .Value }}
  {{- end }}
  {{- $parts := list }}
  {{- with $deprecated.deprecated_in }}
    {{- $id := . | replace "." "" }}
    {{- $parts = append $parts (printf "since version <a href=\"https://docs.victoriametrics.com/operator/changelog/#%s\">%s</a>" $id .) }}
  {{- end }}
  {{- with $deprecated.removed_in }}
    {{- $id := . | replace "." "" }}
    {{- $parts = append $parts (printf "will be removed in <a href=\"https://docs.victoriametrics.com/operator/changelog/#%s\">%s</a>" $id .) }}
  {{- end }}
  {{- with $deprecated.replacements }}
    {{- $links := list }}
    {{- range . }}
      {{- $id := lower (ternary (replace "." "-" .) (printf "%s-%s" $type .) (contains "." .)) }}
      {{- $links = append $links (printf "<a href=\"#%s\">%s</a>" $id (. | splitList "." | last)) }}
    {{- end }}
    {{- $parts = append $parts (printf "use %s instead" (join ", " $links)) }}
  {{- end }}
  {{- with $parts }}<br/><b>Deprecated: </b>{{ join " " . }}<br/>{{ end }}
{{- end -}}


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

{{- if $type.References -}}
Appears in: {{ range $i, $ref := $type.SortedReferences }}{{ if $i }}, {{ end }}{{ markdownRenderTypeLink $ref }}{{- end }}
{{- end }}

{{ if $type.Members -}}
| Field | Description |
| --- | --- |
{{ if $type.GVK -}}
| apiVersion<br/>_string_ | (Required)<br/>`{{ $type.GVK.Group }}/{{ $type.GVK.Version }}` |
| kind<br/>_string_ | (Required)<br/>`{{ $type.GVK.Kind }}` |
{{ end -}}
{{- $members := default dict -}}
{{- range $member := $type.Members -}}
{{- $_ := set $members $member.Name $member }}
{{- end -}}
{{- $memberKeys := (keys $members | sortAlpha) -}}
{{ range $memberKeys -}}
{{- $member := index $members . -}}
{{- $id := lower (printf "%s-%s" $type.Name $member.Name) -}}
| {{ $member.Name }}<a href="#{{ $id }}" id="{{ $id }}">#</a><br/>_{{ markdownRenderType $member.Type }}_ | {{ if $member.Markers.optional }}_(Optional)_<br/>{{else}}_(Required)_<br/>{{ end }}{{ template "type_members" $member }}{{ template "deprecated" (dict "member" $member "type" $type.Name) }} |
{{ end -}}

{{- end -}}
{{- end -}}
{{- end -}}
