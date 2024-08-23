{{- define "name" -}}
    istio-cni
{{- end }}


{{- define "istio-tag" -}}
    {{ .Values.tag | default .Values.global.tag }}{{with (.Values.variant | default .Values.global.variant)}}-{{.}}{{end}}
{{- end }}


{{- define "common-install-cni" }}
    # This container installs the Istio CNI binaries
    # and CNI network config file on each node.
    - name: install-cni
{{- if contains "/" .Values.image }}
    image: "{{ .Values.image }}"
{{- else }}
    image: "{{ .Values.hub | default .Values.global.hub }}/{{ .Values.image | default "install-cni" }}:{{ template "istio-tag" . }}"
{{- end }}
{{- if or .Values.pullPolicy .Values.global.imagePullPolicy }}
    imagePullPolicy: {{ .Values.pullPolicy | default .Values.global.imagePullPolicy }}
{{- end }}
    securityContext:
    privileged: true # always requires privilege to be useful (install node plugin, etc)
    runAsGroup: 0
    runAsUser: 0
    runAsNonRoot: false
    # Both ambient and sidecar repair mode require elevated node privileges to function.
    # But we don't need _everything_ in `privileged`, so drop+readd capabilities based on feature.
    # privileged is redundant with CAP_SYS_ADMIN
    # since it's redundant, hardcode it to `true`, then manually drop ALL + readd granular
    # capabilities we actually require
    capabilities:
        drop:
        - ALL
        add:
        # CAP_NET_ADMIN is required to allow ipset and route table access
        - NET_ADMIN
        # CAP_NET_RAW is required to allow iptables mutation of the `nat` table
        - NET_RAW
        # CAP_SYS_ADMIN is required for both ambient and repair, in order to open
        # network namespaces in `/proc` to obtain descriptors for entering pod netnamespaces.
        # There does not appear to be a more granular capability for this.
        - SYS_ADMIN
{{- if .Values.seccompProfile }}
            seccompProfile:
{{ toYaml .Values.seccompProfile | trim | indent 14 }}
{{- end }}
    command: ["install-cni"]
        args:
        {{- if or .Values.logging.level .Values.global.logging.level }}
        - --log_output_level={{ coalesce .Values.logging.level .Values.global.logging.level }}
        {{- end}}
        {{- if .Values.global.logAsJson }}
        - --log_as_json
        {{- end}}
        envFrom:
        - configMapRef:
            name: {{ template "name" . }}-config
        env:
        - name: REPAIR_NODE_NAME
            valueFrom:
            fieldRef:
                fieldPath: spec.nodeName
        - name: REPAIR_RUN_AS_DAEMON
            value: "true"
        - name: REPAIR_SIDECAR_ANNOTATION
            value: "sidecar.istio.io/status"
        - name: NODE_NAME
            valueFrom:
            fieldRef:
                apiVersion: v1
                fieldPath: spec.nodeName
        - name: GOMEMLIMIT
            valueFrom:
            resourceFieldRef:
                resource: limits.memory
        - name: GOMAXPROCS
            valueFrom:
            resourceFieldRef:
                resource: limits.cpu
        - name: POD_NAME
            valueFrom:
            fieldRef:
                fieldPath: metadata.name
        - name: POD_NAMESPACE
            valueFrom:
            fieldRef:
                fieldPath: metadata.namespace
        {{- if not .Values.privileged }}
        - name: INIT_ONLY
            value: "true"
        {{- end }}
        volumeMounts:
        - mountPath: /host/opt/cni/bin
            name: cni-bin-dir
        {{- if or .Values.repair.repairPods .Values.ambient.enabled }}
        - mountPath: /host/proc
            name: cni-host-procfs
            readOnly: true
        {{- end }}
        - mountPath: /host/etc/cni/net.d
            name: cni-net-dir
        - mountPath: /var/run/istio-cni
            name: cni-socket-dir
        {{- if .Values.ambient.enabled }}
        - mountPath: /host/var/run/netns
            mountPropagation: HostToContainer
            name: cni-netns-dir
        - mountPath: /var/run/ztunnel
            name: cni-ztunnel-sock-dir
        {{ end }}
        resources:
{{- if .Values.resources }}
{{ toYaml .Values.resources | trim | indent 12 }}
{{- else }}
{{ toYaml .Values.global.defaultResources | trim | indent 12 }}
{{- end }}
{{- end }}
