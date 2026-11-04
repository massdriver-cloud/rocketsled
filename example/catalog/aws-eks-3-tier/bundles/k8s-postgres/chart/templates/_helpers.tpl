{{- define "pg.name" -}}
{{ .Release.Name | trunc 63 | trimSuffix "-" }}
{{- end -}}

{{- define "pg.labels" -}}
app.kubernetes.io/name: postgres
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: massdriver
{{- range $k, $v := .Values.commonLabels }}
{{ $k }}: {{ $v | quote }}
{{- end }}
{{- end -}}

{{- define "pg.selector" -}}
app.kubernetes.io/name: postgres
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/* Reuse the password already in the cluster so upgrades never rotate it. */}}
{{- define "pg.password" -}}
{{- $existing := lookup "v1" "Secret" .Release.Namespace (include "pg.name" .) -}}
{{- if and $existing (index $existing.data "password") -}}
{{ index $existing.data "password" | b64dec }}
{{- else -}}
{{ randAlphaNum 32 }}
{{- end -}}
{{- end -}}

{{- define "pg.resources" -}}
{{- if eq .Values.size "large" -}}
requests: {cpu: "1", memory: 2Gi}
limits: {cpu: "2", memory: 4Gi}
{{- else if eq .Values.size "medium" -}}
requests: {cpu: 500m, memory: 1Gi}
limits: {cpu: "1", memory: 2Gi}
{{- else -}}
requests: {cpu: 100m, memory: 256Mi}
limits: {cpu: 250m, memory: 512Mi}
{{- end -}}
{{- end -}}
