{{/*
Expand the name of the chart.
*/}}
{{- define "kubeopencode.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "kubeopencode.fullname" -}}
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
{{- define "kubeopencode.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "kubeopencode.labels" -}}
helm.sh/chart: {{ include "kubeopencode.chart" . }}
{{ include "kubeopencode.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- with .Values.commonLabels }}
{{ toYaml . }}
{{- end }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "kubeopencode.selectorLabels" -}}
app.kubernetes.io/name: {{ include "kubeopencode.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Controller labels
*/}}
{{- define "kubeopencode.controller.labels" -}}
{{ include "kubeopencode.labels" . }}
app.kubernetes.io/component: controller
{{- end }}

{{/*
Controller selector labels
*/}}
{{- define "kubeopencode.controller.selectorLabels" -}}
{{ include "kubeopencode.selectorLabels" . }}
app.kubernetes.io/component: controller
{{- end }}

{{/*
Create the name of the controller service account to use
*/}}
{{- define "kubeopencode.controller.serviceAccountName" -}}
{{- if .Values.controller.serviceAccount.name }}
{{- .Values.controller.serviceAccount.name }}
{{- else }}
{{- printf "%s-controller" (include "kubeopencode.fullname" .) }}
{{- end }}
{{- end }}

{{/*
Controller image
*/}}
{{- define "kubeopencode.controller.image" -}}
{{- $tag := .Values.controller.image.tag | default .Chart.AppVersion }}
{{- printf "%s:%s" .Values.controller.image.repository $tag }}
{{- end }}

{{/*
Agent image
*/}}
{{- define "kubeopencode.agent.image" -}}
{{- printf "%s:%s" .Values.agent.image.repository .Values.agent.image.tag }}
{{- end }}

{{/*
Namespace
*/}}
{{- define "kubeopencode.namespace" -}}
{{- default .Release.Namespace .Values.namespaceOverride }}
{{- end }}

{{/*
Webhook labels
*/}}
{{- define "kubeopencode.webhook.labels" -}}
{{ include "kubeopencode.labels" . }}
app.kubernetes.io/component: webhook
{{- end }}

{{/*
Webhook selector labels
*/}}
{{- define "kubeopencode.webhook.selectorLabels" -}}
{{ include "kubeopencode.selectorLabels" . }}
app.kubernetes.io/component: webhook
{{- end }}
