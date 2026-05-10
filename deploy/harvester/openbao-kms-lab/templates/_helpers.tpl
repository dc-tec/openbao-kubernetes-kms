{{- define "openbao-kms-harvester-lab.name" -}}
{{- .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "openbao-kms-harvester-lab.labels" -}}
app.kubernetes.io/name: {{ include "openbao-kms-harvester-lab.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/component: harvester-kubeadm-lab
{{- end -}}

{{- define "openbao-kms-harvester-lab.vmLabels" -}}
{{ include "openbao-kms-harvester-lab.labels" .root }}
openbao-kms.dev/lab-role: {{ .vm.role | quote }}
vm-name: {{ .vm.name | quote }}
{{- end -}}
