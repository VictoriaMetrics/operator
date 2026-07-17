package vl

import (
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/migrate"
)

func bufferAgentWriteURL(remoteWriteURL, pathPrefix string) string {
	return strings.TrimSuffix(remoteWriteURL, "/insert/native") + pathPrefix + "/internal/insert"
}

func normalizePathPrefix(p string) string {
	p = strings.Trim(p, "/")
	if p == "" {
		return ""
	}
	return "/" + p
}

func newBufferAgent(name, namespace, remoteWriteURL, bufferSize, pathPrefix string) (*vmv1.VLAgent, error) {
	size, err := migrate.ParseAgentBufferSize(bufferSize)
	if err != nil {
		return nil, err
	}
	var extraArgs map[string]string
	if pathPrefix != "" {
		extraArgs = map[string]string{"http.pathPrefix": pathPrefix}
	}
	return &vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: vmv1.VLAgentSpec{
			CommonAppsParams: vmv1beta1.CommonAppsParams{ExtraArgs: extraArgs},
			Storage:          migrate.BufferAgentStorage(size),
			RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
				{URL: remoteWriteURL},
			},
		},
	}, nil
}
