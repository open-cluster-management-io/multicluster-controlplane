{{/* validate service */}}
{{- define "validate.exposeService" }}
    {{- if .Values.route.enabled }}
        {{- if or .Values.loadbalancer.enabled .Values.nodeport.enabled }}
            {{- fail "route, loadbalancer and nodeport should not be enabled more than 1" }}
        {{- end }}

    {{- else if .Values.loadbalancer.enabled }}
        {{- if or .Values.route.enabled .Values.nodeport.enabled }}
            {{- fail "route, loadbalancer and nodeport should not be enabled more than 1" }}
        {{- end }}
        {{- if not .Values.loadbalancer.baseDomain }}
            {{- fail "loadbalancer.baseDomain should be set while loadbalancer is enabled" }}
        {{- end }}

    {{- else if .Values.nodeport.enabled }}
        {{- if or .Values.route.enabled .Values.loadbalancer.enabled }}
            {{- fail "route, loadbalancer and nodeport should not be enabled more than 1" }}
        {{- end }} 
        {{- if not .Values.nodeport.port }}
            {{- fail "nodeport.port should be set while nodeport is enabled" }}
        {{- end }}
    {{- else }}
        {{/* service not exposed */}}
    {{- end }}
{{- end }}
