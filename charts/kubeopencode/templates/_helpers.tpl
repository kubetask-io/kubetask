{{/*
Expand the name of the chart.
*/}}
{{- define "kubetask.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "kubetask.fullname" -}}
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
{{- define "kubetask.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "kubetask.labels" -}}
helm.sh/chart: {{ include "kubetask.chart" . }}
{{ include "kubetask.selectorLabels" . }}
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
{{- define "kubetask.selectorLabels" -}}
app.kubernetes.io/name: {{ include "kubetask.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Controller labels
*/}}
{{- define "kubetask.controller.labels" -}}
{{ include "kubetask.labels" . }}
app.kubernetes.io/component: controller
{{- end }}

{{/*
Controller selector labels
*/}}
{{- define "kubetask.controller.selectorLabels" -}}
{{ include "kubetask.selectorLabels" . }}
app.kubernetes.io/component: controller
{{- end }}

{{/*
Create the name of the controller service account to use
*/}}
{{- define "kubetask.controller.serviceAccountName" -}}
{{- if .Values.controller.serviceAccount.name }}
{{- .Values.controller.serviceAccount.name }}
{{- else }}
{{- printf "%s-controller" (include "kubetask.fullname" .) }}
{{- end }}
{{- end }}

{{/*
Controller image
*/}}
{{- define "kubetask.controller.image" -}}
{{- $tag := .Values.controller.image.tag | default .Chart.AppVersion }}
{{- printf "%s:%s" .Values.controller.image.repository $tag }}
{{- end }}

{{/*
Agent image
*/}}
{{- define "kubetask.agent.image" -}}
{{- printf "%s:%s" .Values.agent.image.repository .Values.agent.image.tag }}
{{- end }}

{{/*
Namespace
*/}}
{{- define "kubetask.namespace" -}}
{{- default .Release.Namespace .Values.namespaceOverride }}
{{- end }}

{{/*
Webhook labels
*/}}
{{- define "kubetask.webhook.labels" -}}
{{ include "kubetask.labels" . }}
app.kubernetes.io/component: webhook
{{- end }}

{{/*
Webhook selector labels
*/}}
{{- define "kubetask.webhook.selectorLabels" -}}
{{ include "kubetask.selectorLabels" . }}
app.kubernetes.io/component: webhook
{{- end }}
