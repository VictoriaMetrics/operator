package e2e

import (
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"testing"
	"time"
)

func WaitForSts(t *testing.T, kubeclient kubernetes.Interface, namespace, name string, replicas int,
	retryInterval, timeout time.Duration) error {
	err := wait.Poll(retryInterval, timeout, func() (done bool, err error) {
		sts, err := kubeclient.AppsV1().StatefulSets(namespace).Get(name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				t.Logf("Waiting for availability of StatefulSet: %s in Namespace: %s \n", name, namespace)
				return false, nil
			}
			return false, err
		}

		if int(sts.Status.ReadyReplicas) >= replicas {
			return true, nil
		}
		t.Logf("Waiting for full availability of %s sts (%d/%d)\n", name,
			sts.Status.ReadyReplicas, replicas)
		return false, nil
	})
	if err != nil {
		return err
	}
	t.Logf("StatefulSet available (%d/%d)\n", replicas, replicas)
	return nil
}

func WaitForService(t *testing.T, kubeclient kubernetes.Interface, namespace, name string,
	retryInterval, timeout time.Duration) error {
	err := wait.Poll(retryInterval, timeout, func() (done bool, err error) {
		_, err = kubeclient.CoreV1().Services(namespace).Get(name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				t.Logf("Waiting for availability of Service: %s in Namespace: %s \n", name, namespace)
				return false, nil
			}
			return false, err
		}

		return true, nil
	})
	if err != nil {
		return err
	}
	t.Logf("Service available (%s)\n", name)
	return nil
}

func WaitForConfigMap(t *testing.T, kubeclient kubernetes.Interface, namespace, name string,
	retryInterval, timeout time.Duration) error {
	err := wait.Poll(retryInterval, timeout, func() (done bool, err error) {
		_, err = kubeclient.CoreV1().ConfigMaps(namespace).Get(name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				t.Logf("Waiting for availability of ConfigMap: %s in Namespace: %s \n", name, namespace)
				return false, nil
			}
			return false, err
		}

		return true, nil
	})
	if err != nil {
		return err
	}
	t.Logf("StatefulSet available %s\n", name)
	return nil
}

func WaitForSecret(t *testing.T, kubeclient kubernetes.Interface, namespace, name string,
	retryInterval, timeout time.Duration) error {
	err := wait.Poll(retryInterval, timeout, func() (done bool, err error) {
		_, err = kubeclient.CoreV1().Secrets(namespace).Get(name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				t.Logf("Waiting for availability of Secret: %s in Namespace: %s \n", name, namespace)
				return false, nil
			}
			return false, err
		}

		return true, nil
	})
	if err != nil {
		return err
	}
	t.Logf("Secret available %s\n", name)
	return nil
}
