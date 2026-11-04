{{- define "mdb.name" -}}
{{ .Release.Name | trunc 63 | trimSuffix "-" }}
{{- end -}}

{{- define "mdb.labels" -}}
app.kubernetes.io/name: mariadb
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: massdriver
{{- range $k, $v := .Values.commonLabels }}
{{ $k }}: {{ $v | quote }}
{{- end }}
{{- end -}}

{{- define "mdb.selector" -}}
app.kubernetes.io/name: mariadb
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/* Reuse existing passwords so upgrades never rotate them. */}}
{{- define "mdb.secretValue" -}}
{{- $root := index . 0 -}}
{{- $key := index . 1 -}}
{{- $existing := lookup "v1" "Secret" $root.Release.Namespace (include "mdb.name" $root) -}}
{{- if and $existing (index $existing.data $key) -}}
{{ index $existing.data $key | b64dec }}
{{- else -}}
{{ randAlphaNum 32 }}
{{- end -}}
{{- end -}}

{{- define "mdb.resources" -}}
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
