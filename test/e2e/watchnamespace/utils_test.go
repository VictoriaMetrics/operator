package watchnamespace

import (
	"context"
	"reflect"

	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func CreateObjects(objects ...client.Object) {
	for _, obj := range objects {
		ExpectWithOffset(1, k8sClient.Create(context.Background(), obj)).ToNot(HaveOccurred())
	}
}

func DeleteAllObjectsOf(namespace string, listProtos ...client.ObjectList) {
	nsOption := client.InNamespace(namespace)
	for _, listProto := range listProtos {
		objType := GetListObjectType(listProto)
		proto := reflect.New(objType).Interface()
		ExpectWithOffset(1, k8sClient.DeleteAllOf(context.Background(), proto.(client.Object), nsOption)).ToNot(HaveOccurred())
	}

	EventuallyWithOffset(1, func() bool {
		for _, listProto := range listProtos {
			if objects := listObjectsByListProto(namespace, listProto); len(objects) > 0 {
				return false
			}
		}

		return true
	}, 60, 1).Should(BeTrue())
}

func ListObjectsInNamespace(namespace string, listProtos []client.ObjectList) []client.Object {
	var result []client.Object
	for _, listProto := range listProtos {
		objects := listObjectsByListProto(namespace, listProto)
		result = append(result, objects...)
	}

	return result
}

func EventuallyShouldHaveFinalizer(namespace string, listProto client.ObjectList) {
	// EventuallyShouldHaveFinalizer and ConsistentlyShouldNotHaveFinalizer functions are being
	// used to detect operator activity or the lack of it. They rely on a fact that first thing
	// the operator does is to add a finalizer to controlled objects. If the operator changes
	// its behaviour these functions will be unusable for the purpose of checking WATCH_NAMESPACE
	// restriction.

	EventuallyWithOffset(1, func() []client.Object {
		objects := listObjectsByListProto(namespace, listProto)
		Expect(objects).NotTo(BeEmpty())

		var objectsWithoutFinalizers []client.Object
		for _, object := range objects {
			finalizers := object.GetFinalizers()
			if len(finalizers) != 1 || finalizers[0] != vmv1beta1.FinalizerName {
				objectsWithoutFinalizers = append(objectsWithoutFinalizers, object)
			}
		}

		return objectsWithoutFinalizers
	}, 30, 1).Should(BeEmpty())
}

func ConsistentlyShouldNotHaveFinalizer(namespace string, listProtos ...client.ObjectList) {
	// sorry, this has to be slow because we're checking for something not to be done
	ConsistentlyWithOffset(1, func() []client.Object {
		var objectsWithFinalizers []client.Object
		for _, listProto := range listProtos {
			objects := listObjectsByListProto(namespace, listProto)
			Expect(objects).NotTo(BeEmpty())

			for _, object := range objects {
				if len(object.GetFinalizers()) > 0 {
					objectsWithFinalizers = append(objectsWithFinalizers, object)
				}
			}
		}

		return objectsWithFinalizers
	}, 30, 1).Should(BeEmpty())
}

func GetListObjectType(list client.ObjectList) reflect.Type {
	objType := reflect.ValueOf(list).Elem().FieldByName("Items").Type().Elem()
	if objType.Kind() == reflect.Ptr {
		objType = objType.Elem()
	}

	return objType
}

func listObjectsByListProto(namespace string, listProto client.ObjectList) []client.Object {
	var objects []client.Object

	list := listProto.DeepCopyObject().(client.ObjectList)
	Expect(k8sClient.List(context.Background(), list, client.InNamespace(namespace))).ToNot(HaveOccurred())
	itemsValue := reflect.ValueOf(list).Elem().FieldByName("Items")
	for i := 0; i < itemsValue.Len(); i++ {
		itemValue := itemsValue.Index(i)
		if itemValue.Kind() != reflect.Ptr {
			itemValue = itemValue.Addr()
		}

		objects = append(objects, itemValue.Interface().(client.Object))
	}

	return objects
}
