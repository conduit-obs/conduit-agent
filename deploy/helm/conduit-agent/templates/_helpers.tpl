{{/*
Standard Helm helpers. Naming follows the upstream `helm create` template
so operators familiar with other charts find what they expect.
*/}}

{{/*
Expand the name of the chart.
*/}}
{{- define "conduit-agent.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Fully-qualified app name. Includes the release name unless the user
passed nameOverride. Truncated to 63 chars per RFC 1123 (Kubernetes
DNS label limit).
*/}}
{{- define "conduit-agent.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Chart name and version label.
*/}}
{{- define "conduit-agent.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Common labels — applied to every chart-rendered object so `kubectl get -l
app.kubernetes.io/name=conduit-agent -A` finds the whole release.
*/}}
{{- define "conduit-agent.labels" -}}
helm.sh/chart: {{ include "conduit-agent.chart" . }}
{{ include "conduit-agent.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: conduit
{{- end -}}

{{/*
Selector labels — the subset that goes into matchLabels. Must be stable
across upgrades (anything in here gets locked in by the DaemonSet's
selector).
*/}}
{{- define "conduit-agent.selectorLabels" -}}
app.kubernetes.io/name: {{ include "conduit-agent.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
ServiceAccount name. Honors values.serviceAccount.name when set; falls
back to the fullname helper.
*/}}
{{- define "conduit-agent.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "conduit-agent.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/*
Image reference. Falls back to .Chart.AppVersion when image.tag is
unset so `helm install` without --set image.tag tracks the chart's
pinned agent build.
*/}}
{{- define "conduit-agent.image" -}}
{{- $tag := .Values.image.tag | default .Chart.AppVersion -}}
{{- printf "%s:%s" .Values.image.repository $tag -}}
{{- end -}}

{{/*
Output mode (honeycomb | gateway). gateway.enabled wins when set so a
single --set flag is enough to flip a release between Honeycomb-direct
and a customer-operated gateway.
*/}}
{{- define "conduit-agent.outputMode" -}}
{{- if .Values.gateway.enabled -}}
gateway
{{- else -}}
honeycomb
{{- end -}}
{{- end -}}

{{/*
Name of the Secret holding HONEYCOMB_API_KEY. Returns
honeycomb.existingSecret when set, otherwise the chart-managed Secret
name (rendered by templates/secret.yaml when honeycomb.apiKey is
populated).
*/}}
{{- define "conduit-agent.honeycombSecretName" -}}
{{- if .Values.honeycomb.existingSecret -}}
{{- .Values.honeycomb.existingSecret -}}
{{- else -}}
{{- include "conduit-agent.fullname" . -}}
{{- end -}}
{{- end -}}
