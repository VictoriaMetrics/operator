package main

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
)

func TestSecretFromEvent(t *testing.T) {
	secret := &corev1.Secret{}

	t.Run("secret object", func(t *testing.T) {
		got, ok := secretFromEvent(secret)
		if !ok {
			t.Fatal("expected secret object to be accepted")
		}
		if got != secret {
			t.Fatal("expected original secret pointer to be returned")
		}
	})

	t.Run("deleted final state unknown value", func(t *testing.T) {
		got, ok := secretFromEvent(cache.DeletedFinalStateUnknown{Obj: secret})
		if !ok {
			t.Fatal("expected tombstone value to be accepted")
		}
		if got != secret {
			t.Fatal("expected tombstone secret pointer to be returned")
		}
	})

	t.Run("deleted final state unknown pointer", func(t *testing.T) {
		got, ok := secretFromEvent(&cache.DeletedFinalStateUnknown{Obj: secret})
		if !ok {
			t.Fatal("expected tombstone pointer to be accepted")
		}
		if got != secret {
			t.Fatal("expected tombstone secret pointer to be returned")
		}
	})

	t.Run("unexpected type", func(t *testing.T) {
		if _, ok := secretFromEvent("not-a-secret"); ok {
			t.Fatal("expected unexpected object type to be rejected")
		}
	})
}
