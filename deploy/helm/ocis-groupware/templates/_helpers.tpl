{{- define "ocis-groupware.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "ocis-groupware.fullname" -}}
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

{{- define "ocis-groupware.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{ include "ocis-groupware.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "ocis-groupware.selectorLabels" -}}
app.kubernetes.io/name: {{ include "ocis-groupware.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "ocis-groupware.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "ocis-groupware.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/* The port number in config.httpAddr, for example 8080 for ":8080". */}}
{{- define "ocis-groupware.port" -}}
{{- regexFind "[0-9]+$" .Values.config.httpAddr | required "config.httpAddr must end in a port number, for example \":8080\"" }}
{{- end }}

{{/* The port number in config.debugAddr, or empty when the debug listener is off. */}}
{{- define "ocis-groupware.debugPort" -}}
{{- if .Values.config.debugAddr -}}
{{- regexFind "[0-9]+$" .Values.config.debugAddr | required "config.debugAddr must end in a port number, for example \":9180\"" }}
{{- end -}}
{{- end }}

{{- define "ocis-groupware.image" -}}
{{- printf "%s:%s" .Values.image.repository (default .Chart.AppVersion .Values.image.tag) }}
{{- end }}

{{- define "ocis-groupware.claimName" -}}
{{- default (include "ocis-groupware.fullname" .) .Values.persistence.existingClaim }}
{{- end }}

{{- define "ocis-groupware.caConfigMapName" -}}
{{- default (printf "%s-ca" (include "ocis-groupware.fullname" .)) .Values.oidcCA.existingConfigMap }}
{{- end }}

{{- define "ocis-groupware.legacySecretName" -}}
{{- default (printf "%s-legacy-token" (include "ocis-groupware.fullname" .)) .Values.legacyTokenSecret.existingSecret }}
{{- end }}

{{/* The directory of the admin socket when it is set outside the data directory, else empty. */}}
{{- define "ocis-groupware.socketDir" -}}
{{- with .Values.config.adminSocket }}
{{- if ne (dir .) ($.Values.persistence.mountPath | clean) }}{{ dir . }}{{ end }}
{{- end }}
{{- end }}
