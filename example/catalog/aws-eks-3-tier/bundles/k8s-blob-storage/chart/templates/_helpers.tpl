{{- define "minio.name" -}}
{{ .Release.Name | trunc 63 | trimSuffix "-" }}
{{- end -}}

{{- define "minio.labels" -}}
app.kubernetes.io/name: minio
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: massdriver
{{- range $k, $v := .Values.commonLabels }}
{{ $k }}: {{ $v | quote }}
{{- end }}
{{- end -}}

{{- define "minio.selector" -}}
app.kubernetes.io/name: minio
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "minio.secretValue" -}}
{{- $root := index . 0 -}}
{{- $key := index . 1 -}}
{{- $length := index . 2 -}}
{{- $existing := lookup "v1" "Secret" $root.Release.Namespace (include "minio.name" $root) -}}
{{- if and $existing (index $existing.data $key) -}}
{{ index $existing.data $key | b64dec }}
{{- else -}}
{{ randAlphaNum $length }}
{{- end -}}
{{- end -}}

{{- define "minio.resources" -}}
{{- if eq .Values.size "large" -}}
requests: {cpu: 500m, memory: 1Gi}
limits: {cpu: "1", memory: 2Gi}
{{- else -}}
requests: {cpu: 100m, memory: 256Mi}
limits: {cpu: 250m, memory: 512Mi}
{{- end -}}
{{- end -}}
