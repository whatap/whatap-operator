package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const networkEnvFeature = `{"enabled":true,"image":"example.invalid/network:env","env":[{"name":"WHATAP_STDOUT","value":"none"},{"name":"WHATAP_LOG_LEVEL","valueFrom":{"configMapKeyRef":{"name":"logging","key":"level","optional":true}}},{"name":"WHATAP_LOG_INTERVAL","valueFrom":{"secretKeyRef":{"name":"logging-secret","key":"interval"}}},{"name":"ORDER","value":"first"},{"name":"DEPENDENT","value":"$(ORDER)-$(NODE_NAME)"},{"name":"ORDER","value":"last"},{"name":"POD_NAME","valueFrom":{"fieldRef":{"fieldPath":"metadata.name"}}}],"envFrom":[{"prefix":"CUSTOM_","configMapRef":{"name":"logging","optional":true}},{"secretRef":{"name":"logging-secret","optional":false}}]}`

type networkEnvCapture struct {
	client.Client
	desired *appsv1.DaemonSet
}

func (c *networkEnvCapture) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if ds, ok := obj.(*appsv1.DaemonSet); ok {
		c.desired = ds
	}
	return c.Client.Create(ctx, obj, opts...)
}

func TestNetworkAgentEnvPassThrough(t *testing.T) {
	r, cr := networkFixture(t, networkEnvFeature)
	before, _ := json.Marshal(cr)
	capture := &networkEnvCapture{Client: r.Client}
	r.Client = capture
	if err := r.reconcileNetworkAgent(context.Background(), cr); err != nil {
		t.Fatal(err)
	}
	c := networkDS(t, r).Spec.Template.Spec.Containers[0]
	var want struct {
		Env     []corev1.EnvVar        `json:"env"`
		EnvFrom []corev1.EnvFromSource `json:"envFrom"`
	}
	if err := json.Unmarshal([]byte(networkEnvFeature), &want); err != nil {
		t.Fatal(err)
	}
	want.Env[6].ValueFrom.FieldRef.APIVersion = "v1"
	if len(c.Env) != 7+len(want.Env) {
		t.Fatalf("custom env lost: got %d entries", len(c.Env))
	}
	if !reflect.DeepEqual(c.Env[7:], want.Env) || !reflect.DeepEqual(c.EnvFrom, want.EnvFrom) {
		t.Fatal("ordered env/source pass-through changed")
	}
	if len(c.Args) != 5 {
		t.Fatal("omitted logging fields injected CLI overrides")
	}
	if c.Env[0].Name != "NODE_NAME" || c.Env[6].Name != "GOMEMLIMIT" {
		t.Fatal("managed explicit env precedence lost")
	}
	after, _ := json.Marshal(cr)
	if string(before) != string(after) {
		t.Fatal("defaulting mutated CR")
	}
	capture.desired.Spec.Template.Spec.Containers[0].Env[8].ValueFrom.ConfigMapKeyRef.Name = "mutated"
	*capture.desired.Spec.Template.Spec.Containers[0].EnvFrom[0].ConfigMapRef.Optional = false
	after, _ = json.Marshal(cr)
	if string(before) != string(after) {
		t.Fatal("generated DaemonSet aliases CR")
	}
	clone := cr.DeepCopy()
	b, _ := json.Marshal(clone)
	if !reflect.DeepEqual(before, b) {
		t.Fatal("CR deep-copy lost env")
	}
	clone.Spec.Features.NetworkAgent.Env[1].ValueFrom.ConfigMapKeyRef.Name = "mutated"
	*clone.Spec.Features.NetworkAgent.EnvFrom[0].ConfigMapRef.Optional = false
	after, _ = json.Marshal(cr)
	if string(before) != string(after) {
		t.Fatal("CR deep-copy aliases env")
	}
}

func TestNetworkAgentEnvOmittedCompatibility(t *testing.T) {
	r, cr := networkFixture(t, `{"enabled":true,"image":"example.invalid/network:v1"}`)
	if err := r.reconcileNetworkAgent(context.Background(), cr); err != nil {
		t.Fatal(err)
	}
	first := networkDS(t, r)
	c := first.Spec.Template.Spec.Containers[0]
	if len(c.Env) != 7 || c.EnvFrom != nil || len(c.Args) != 5 {
		t.Fatal("omitted fields changed legacy container")
	}
	if err := r.reconcileNetworkAgent(context.Background(), cr); err != nil {
		t.Fatal(err)
	}
	after := networkDS(t, r)
	if first.ResourceVersion != after.ResourceVersion || !reflect.DeepEqual(first.Spec.Template, after.Spec.Template) {
		t.Fatal("unchanged template is not idempotent")
	}
}

func TestNetworkAgentEnvRejectsBeforeWrites(t *testing.T) {
	cases := []struct{ name, fields, path string }{}
	for _, name := range []string{"NODE_NAME", "WHATAP_OBJECT_NAME", "WHATAP_ACCESSKEY", "WHATAP_SERVER_HOST", "WHATAP_SERVER_PORT", "GOMAXPROCS", "GOMEMLIMIT"} {
		cases = append(cases, struct{ name, fields, path string }{name, fmt.Sprintf(`"env":[{"name":%q,"value":"REDACT_ME"}]`, name), "env[0].name"})
	}
	for _, tc := range []struct{ name, fields, path string }{
		{"bad-name", `"env":[{"name":"1BAD","value":"REDACT_ME"}]`, "env[0].name"},
		{"value-source", `"env":[{"name":"CUSTOM","value":"REDACT_ME","valueFrom":{"secretKeyRef":{"name":"logging","key":"key"}}}]`, "env[0].valueFrom"},
		{"empty-source", `"env":[{"name":"CUSTOM","valueFrom":{}}]`, "env[0].valueFrom"},
		{"two-sources", `"env":[{"name":"CUSTOM","valueFrom":{"secretKeyRef":{"name":"s","key":"k"},"configMapKeyRef":{"name":"c","key":"k"}}}]`, "env[0].valueFrom"},
		{"secret-name", `"env":[{"name":"CUSTOM","valueFrom":{"secretKeyRef":{"name":"REDACT_ME","key":"key"}}}]`, "env[0].valueFrom.secretKeyRef.name"},
		{"config-key", `"env":[{"name":"CUSTOM","valueFrom":{"configMapKeyRef":{"name":"logging","key":""}}}]`, "env[0].valueFrom.configMapKeyRef.key"},
		{"field-empty", `"env":[{"name":"CUSTOM","valueFrom":{"fieldRef":{"fieldPath":""}}}]`, "env[0].valueFrom.fieldRef.fieldPath"},
		{"resource-empty", `"env":[{"name":"CUSTOM","valueFrom":{"resourceFieldRef":{"resource":""}}}]`, "env[0].valueFrom.resourceFieldRef.resource"},
		{"from-empty", `"envFrom":[{}]`, "envFrom[0]"},
		{"from-two", `"envFrom":[{"configMapRef":{"name":"logging"},"secretRef":{"name":"logging"}}]`, "envFrom[0]"},
		{"from-name", `"envFrom":[{"secretRef":{"name":"REDACT_ME"}}]`, "envFrom[0].secretRef.name"},
		{"from-prefix", `"envFrom":[{"prefix":"1BAD","configMapRef":{"name":"logging"}}]`, "envFrom[0].prefix"},
	} {
		cases = append(cases, tc)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, cr := networkFixture(t, `{"enabled":true,"image":"example.invalid/network:env",`+tc.fields+`}`)
			err := r.reconcileNetworkAgent(context.Background(), cr)
			if err == nil {
				t.Fatal("invalid env accepted")
			}
			if !strings.Contains(err.Error(), "networkAgent."+tc.path) || strings.Contains(err.Error(), "REDACT_ME") {
				t.Fatalf("not field-specific/redacted: %v", err)
			}
			for _, obj := range r.networkAgentResources(cr) {
				if err := r.Get(context.Background(), client.ObjectKeyFromObject(obj), obj); !apierrors.IsNotFound(err) {
					t.Fatalf("resource written before validation: %T", obj)
				}
			}
		})
	}
}
