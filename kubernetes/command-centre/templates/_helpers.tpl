{{/*
Expand the name of the chart.
*/}}
{{- define "command-centre.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "command-centre.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "command-centre.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "command-centre.labels" -}}
helm.sh/chart: {{ include "command-centre.chart" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: command-centre
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}

{{/*
Backend selector labels
*/}}
{{- define "command-centre.backend.selectorLabels" -}}
app.kubernetes.io/name: {{ include "command-centre.name" . }}-backend
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: backend
{{- end }}

{{/*
Frontend selector labels
*/}}
{{- define "command-centre.frontend.selectorLabels" -}}
app.kubernetes.io/name: {{ include "command-centre.name" . }}-frontend
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: frontend
{{- end }}

{{/*
Backend image
*/}}
{{- define "command-centre.backend.image" -}}
{{- printf "%s:%s" .Values.backend.image.repository .Values.backend.image.tag }}
{{- end }}

{{/*
Frontend image
*/}}
{{- define "command-centre.frontend.image" -}}
{{- printf "%s:%s" .Values.frontend.image.repository .Values.frontend.image.tag }}
{{- end }}
