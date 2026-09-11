{{- define "notes" -}}
  {{- $member := .member }}
  {{- $type := .type }}
  {{- $notes := default dict }}
  {{- range $member.Markers.notes }}
    {{- $notes = mergeOverwrite $notes .Value }}
  {{- end }}
  {{- $parts := list }}
  {{- with $notes.deprecated_in }}
    {{- $id := . | replace "." "" }}
    {{- $parts = append $parts (printf "since version <a href=\"https://docs.victoriametrics.com/operator/changelog/#%s\">%s</a>" $id .) }}
  {{- end }}
  {{- with $notes.removed_in }}
    {{- if eq . "v1.0.0" }}
      {{- $parts = append $parts (printf "will be removed in %s" .) }}
    {{- else }}
      {{- $id := . | replace "." "" }}
      {{- $parts = append $parts (printf "will be removed in <a href=\"https://docs.victoriametrics.com/operator/changelog/#%s\">%s</a>" $id .) }}
    {{- end }}
  {{- end }}
  {{- with $notes.replacements }}
    {{- $links := list }}
    {{- range . }}
      {{- $id := lower (ternary (replace "." "-" .) (printf "%s-%s" $type .) (contains "." .)) }}
      {{- $links = append $links (printf "<a href=\"#%s\">%s</a>" $id (. | splitList "." | last)) }}
    {{- end }}
    {{- $parts = append $parts (printf "use %s instead" (join ", " $links)) }}
  {{- end }}
  {{- with $notes.available_from }}
    {{- $id := . | replace "." "" -}}
    <br/><b>Available from: </b>{{ printf "<a href=\"https://docs.victoriametrics.com/operator/changelog/#%s\">%s</a>" $id . }}
  {{- end }}
  {{- with $parts }}<br/><b>Deprecated: </b>{{ join " " . }}<br/>{{ end }}
{{- end -}}

{{- define "typeLink" -}}
{{- $t := . }}
{{- $leaf := $t }}
{{- if and $t (eq (toJson $t.Kind) "\"POINTER\"") -}}
  {{- $leaf = $t.UnderlyingType -}}
{{- end -}}
{{- if not $leaf -}}
{{- else if markdownShouldRenderType $leaf -}}
  {{- $version := $leaf.Package | splitList "/" | last -}}
  {{- printf "[%s (%s)](#%s)" $leaf.Name $version (lower (printf "%s-%s" $version $leaf.Name)) -}}
{{- else -}}
  {{- markdownRenderTypeLink $t -}}
{{- end -}}
{{- end -}}

{{- define "memberType" -}}
{{- $t := . -}}
{{- if eq (toJson $t.Kind) "\"MAP\"" -}}
object (keys:{{ template "typeLink" $t.KeyType }}, values:{{ template "typeLink" $t.ValueType }})
{{- else if eq (toJson $t.Kind) "\"SLICE\"" -}}
{{ template "typeLink" $t.UnderlyingType }} array
{{- else -}}
{{ template "typeLink" $t }}
{{- end -}}
{{- end -}}


{{- define "type" -}}
{{- $ctx := . -}}
{{- $type := $ctx.type -}}
{{- $aliases := $ctx.aliases -}}
{{- if markdownShouldRenderType $type }}
{{- $version := $type.Package | splitList "/" | last }}
{{- $aliasKey := lower $type.Name }}
{{- $isAliasOwner := eq (index $aliases $aliasKey) $type.Package }}

#### {{ $type.Name }} {#{{ lower (printf "%s-%s" $version $type.Name) }}}
{{- if $isAliasOwner }}
<a id="{{ $aliasKey }}"></a>
{{- end }}
{{- if $type.IsAlias }}

_Underlying type:_ _{{ template "typeLink" $type.UnderlyingType }}_
{{- end }}
{{- if $type.Doc }}

{{ $type.Doc }}
{{- end }}
{{- if $type.Validation }}

_Validation:_
{{- range $type.Validation }}
- {{ . }}
{{- end }}
{{- end }}
{{- if $type.References }}

Appears in: {{ range $i, $ref := $type.SortedReferences }}{{ if $i }}, {{ end }}{{ template "typeLink" $ref }}{{- end }}
{{- end }}
{{- if $type.Members }}

| Field | Description |
| --- | --- |
{{- if $type.GVK }}
| apiVersion<br/>_string_ | (Required)<br/>`{{ $type.GVK.Group }}/{{ $type.GVK.Version }}` |
| kind<br/>_string_ | (Required)<br/>`{{ $type.GVK.Kind }}` |
{{- end }}
{{- $members := default dict }}
{{- range $member := $type.Members }}
{{- $_ := set $members $member.Name $member }}
{{- end }}
{{- $memberKeys := (keys $members | sortAlpha) }}
{{- range $memberKeys }}
{{- $member := index $members . }}
{{- $id := lower (printf "%s-%s-%s" $version $type.Name $member.Name) }}
{{- $oldId := lower (printf "%s-%s" $type.Name $member.Name) }}
| {{ $member.Name }}{{ if $member.Aliases }} _(or {{ range $i, $a := $member.Aliases }}{{ if $i }}, {{ end }}{{ $a }}{{ end }})_ {{ end }}<a href="#{{ $id }}" id="{{ $id }}">#</a>{{- if $isAliasOwner }}<a id="{{ $oldId }}"></a>{{- end }}<br/>_{{ template "memberType" $member.Type }}_ | {{ if $member.Markers.optional }}_(Optional)_<br/>{{else}}_(Required)_<br/>{{ end }}{{ template "type_members" $member }}{{ template "notes" (dict "member" $member "type" $type.Name) }} |
{{- end }}
{{- end }}
{{- end }}
{{- end }}
