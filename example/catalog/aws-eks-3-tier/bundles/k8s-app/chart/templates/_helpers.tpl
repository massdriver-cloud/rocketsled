{{- define "app.name" -}}
{{ .Release.Name | trunc 63 | trimSuffix "-" }}
{{- end -}}

{{- define "app.labels" -}}
app.kubernetes.io/name: {{ include "app.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Values.image.tag | quote }}
app.kubernetes.io/managed-by: massdriver
{{- range $k, $v := .Values.commonLabels }}
{{ $k }}: {{ $v | quote }}
{{- end }}
{{- end -}}

{{- define "app.selector" -}}
app.kubernetes.io/name: {{ include "app.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "app.resources" -}}
{{- if eq .Values.size "large" -}}
requests: {cpu: 500m, memory: 512Mi}
limits: {cpu: "1", memory: 1Gi}
{{- else if eq .Values.size "medium" -}}
requests: {cpu: 250m, memory: 256Mi}
limits: {cpu: 500m, memory: 512Mi}
{{- else -}}
requests: {cpu: 100m, memory: 128Mi}
limits: {cpu: 250m, memory: 256Mi}
{{- end -}}
{{- end -}}
