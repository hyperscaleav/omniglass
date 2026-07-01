{{- define "omniglass.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Fullname defaults to the release name (not release-chart), so preview releases
named og-pr-<n> produce predictable service DNS like og-pr-42 / og-pr-42-postgres.
*/}}
{{- define "omniglass.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "omniglass.labels" -}}
app.kubernetes.io/name: {{ include "omniglass.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
{{- end -}}

{{- define "omniglass.selectorLabels" -}}
app.kubernetes.io/name: {{ include "omniglass.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
The DSN the server and the migrate/bootstrap init containers dial. Bundled
Postgres resolves to the in-release service; otherwise externalDsn is required.
*/}}
{{- define "omniglass.dsn" -}}
{{- if .Values.postgres.enabled -}}
postgres://{{ .Values.postgres.user }}:{{ .Values.postgres.password }}@{{ include "omniglass.fullname" . }}-postgres:5432/{{ .Values.postgres.database }}?sslmode=disable
{{- else -}}
{{- required "externalDsn is required when postgres.enabled is false" .Values.externalDsn -}}
{{- end -}}
{{- end -}}
