{{- define "rwox.name" -}}rwox-csi{{- end -}}
{{- define "rwox.namespace" -}}{{ .Values.namespace }}{{- end -}}
{{- define "rwox.image" -}}
{{- printf "%s:%s" .Values.image.repository (default .Chart.AppVersion .Values.image.tag) -}}
{{- end -}}
