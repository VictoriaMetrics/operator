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

func newBufferAgent(name, namespace, remoteWriteURL, bufferSize, pathPrefix string) (*vmv1.VLAgent, error) {
	size, err := migrate.ParseAgentBufferSize(bufferSize)
	if err != nil {
		return nil, err
	}
	return &vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: vmv1.VLAgentSpec{
			CommonAppsParams: vmv1beta1.CommonAppsParams{ExtraArgs: migrate.ExtraArgsWithPathPrefix(pathPrefix)},
			Storage:          migrate.BufferAgentStorage(size),
			RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
				{URL: remoteWriteURL},
			},
		},
	}, nil
}
