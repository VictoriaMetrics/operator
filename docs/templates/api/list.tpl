{{- define "gvList" -}}
{{- $groupVersions := . -}}
---
weight: 12
title: API Docs
menu:
  docs:
    parent: operator
    weight: 12
aliases:
  - /operator/api/
  - /operator/api/index.html
  - /operator/api.html
---
<!-- this doc autogenerated - don't edit it manually -->

## Packages
{{- range $groupVersions }}
- {{ markdownRenderGVLink . }}
{{- end }}

{{ range $groupVersions }}
{{ template "gvDetails" . }}
{{ end }}

{{- end -}}
