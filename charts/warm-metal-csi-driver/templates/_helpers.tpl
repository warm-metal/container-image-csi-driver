{{/*
Expand the name of the chart.
*/}}
{{- define "warm-metal-csi-driver.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "warm-metal-csi-driver.fullname" -}}
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
{{- define "warm-metal-csi-driver.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "warm-metal-csi-driver.labels" -}}
helm.sh/chart: {{ include "warm-metal-csi-driver.chart" . }}
{{ include "warm-metal-csi-driver.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "warm-metal-csi-driver.nodeplugin.labels" -}}
component: nodeplugin
{{ include "warm-metal-csi-driver.labels" . }}
{{- end }}

{{- define "warm-metal-csi-driver.controllerplugin.labels" -}}
component: controllerplugin
{{ include "warm-metal-csi-driver.labels" . }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "warm-metal-csi-driver.selectorLabels" -}}
app.kubernetes.io/name: {{ include "warm-metal-csi-driver.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "warm-metal-csi-driver.nodeplugin.selectorLabels" -}}
component: nodeplugin
{{ include "warm-metal-csi-driver.selectorLabels" . }}
{{- end }}

{{- define "warm-metal-csi-driver.controllerplugin.selectorLabels" -}}
component: controllerplugin
{{ include "warm-metal-csi-driver.selectorLabels" . }}
{{- end }}