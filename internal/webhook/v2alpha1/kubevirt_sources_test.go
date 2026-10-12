package v2alpha1

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	jsonpatch "github.com/evanphx/json-patch/v5"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestKubeVirtAdmissionPreservesNetworkDisksAndSourceSecret(t *testing.T) {
	for _, sourceType := range []string{"cloudInitNoCloud", "cloudInitConfigDrive"} {
		for _, form := range []string{"userData", "userDataBase64", "secretRef"} {
			t.Run(sourceType+"/"+form, func(t *testing.T) {
				h, req, c := kubeVirtHandlerFixture(t)
				var vm unstructured.Unstructured
				if err := vm.UnmarshalJSON(req.Object.Raw); err != nil {
					t.Fatal(err)
				}
				volumes, source, index, _, err := cloudInitSource(&vm)
				if err != nil {
					t.Fatal(err)
				}
				original := []byte("#cloud-config\ntimezone: Asia/Seoul\n")
				delete(source, "userData")
				switch form {
				case "userData":
					source[form] = string(original)
				case "userDataBase64":
					source[form] = base64.StdEncoding.EncodeToString(original)
				case "secretRef":
					secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "original", Namespace: "vms"}, Data: map[string][]byte{"userdata": original}}
					if err := c.Create(context.Background(), secret); err != nil {
						t.Fatal(err)
					}
					source[form] = map[string]interface{}{"name": "original"}
				}
				volume := volumes[index].(map[string]interface{})
				delete(volume, "cloudInitNoCloud")
				volume[sourceType] = source
				_ = unstructured.SetNestedSlice(vm.Object, volumes, "spec", "template", "spec", "volumes")
				req.Object.Raw, _ = vm.MarshalJSON()
				response := h.Handle(context.Background(), req)
				if !response.Allowed {
					t.Fatalf("denied: %+v", response.Result)
				}
				patchBytes, _ := json.Marshal(response.Patches)
				patch, err := jsonpatch.DecodePatch(patchBytes)
				if err != nil {
					t.Fatal(err)
				}
				patched, err := patch.Apply(req.Object.Raw)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(patched), "test-license-not-real") {
					t.Fatal("patched VM contains license")
				}
				if err := vm.UnmarshalJSON(patched); err != nil {
					t.Fatal(err)
				}
				newVolumes, newSource, _, newType, err := cloudInitSource(&vm)
				if err != nil || newType != sourceType {
					t.Fatal("changed cloud-init type")
				}
				if newSource["networkData"] != "version: 2\nethernets: {}" {
					t.Fatal("network data changed")
				}
				if newVolumes[0].(map[string]interface{})["persistentVolumeClaim"].(map[string]interface{})["claimName"] != "root-disk" {
					t.Fatal("root disk changed")
				}
				disks, _, _ := unstructured.NestedSlice(vm.Object, "spec", "template", "spec", "domain", "devices", "disks")
				if len(disks) != 2 {
					t.Fatal("cloud-init disk not attached alongside root")
				}
				req.Object.Raw = patched
				again := h.Handle(context.Background(), req)
				if !again.Allowed || len(again.Patches) != 0 {
					t.Fatal("reinvocation is not idempotent")
				}
				if form == "secretRef" {
					originalSecret := &corev1.Secret{}
					_ = c.Get(context.Background(), client.ObjectKey{Name: "original", Namespace: "vms"}, originalSecret)
					if string(originalSecret.Data["userdata"]) != string(original) || originalSecret.Immutable != nil {
						t.Fatal("shared source Secret mutated")
					}
				}
			})
		}
	}
}

func TestKubeVirtAdmissionRejectsSecretCollision(t *testing.T) {
	h, req, c := kubeVirtHandlerFixture(t)
	dry := true
	req.DryRun = &dry
	response := h.Handle(context.Background(), req)
	encoded, _ := json.Marshal(response.Patches)
	patch, _ := jsonpatch.DecodePatch(encoded)
	patched, err := patch.Apply(req.Object.Raw)
	if err != nil {
		t.Fatal(err)
	}
	var vm unstructured.Unstructured
	_ = vm.UnmarshalJSON(patched)
	name := vm.GetAnnotations()["monitoring.whatap.com/server-agent-bootstrap"]
	foreign := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "vms"}, Data: map[string][]byte{"userdata": []byte("foreign contents")}}
	if err := c.Create(context.Background(), foreign); err != nil {
		t.Fatal(err)
	}
	req.DryRun = nil
	response = h.Handle(context.Background(), req)
	if response.Allowed {
		t.Fatal("foreign Secret was adopted")
	}
	stored := &corev1.Secret{}
	_ = c.Get(context.Background(), client.ObjectKeyFromObject(foreign), stored)
	if string(stored.Data["userdata"]) != "foreign contents" {
		t.Fatal("foreign Secret overwritten")
	}
}

func TestKubeVirtAdmissionRejectsInvalidSourceWithoutSecretWrites(t *testing.T) {
	for _, source := range []string{
		`{"userData":"x","userDataBase64":"eA=="}`,
		`{"userDataBase64":"%%%"}`,
		`{"secretRef":{"name":"missing"}}`,
		`{"userData":123}`,
	} {
		h, req, c := kubeVirtHandlerFixture(t)
		var vm unstructured.Unstructured
		_ = vm.UnmarshalJSON(req.Object.Raw)
		volumes, _, index, _, _ := cloudInitSource(&vm)
		var value map[string]interface{}
		_ = json.Unmarshal([]byte(source), &value)
		volumes[index].(map[string]interface{})["cloudInitNoCloud"] = value
		_ = unstructured.SetNestedSlice(vm.Object, volumes, "spec", "template", "spec", "volumes")
		req.Object.Raw, _ = vm.MarshalJSON()
		response := h.Handle(context.Background(), req)
		if response.Allowed {
			t.Fatal("accepted invalid cloud-init source")
		}
		secrets := &corev1.SecretList{}
		_ = c.List(context.Background(), secrets)
		if len(secrets.Items) != 1 {
			t.Fatal("invalid admission created Secret")
		}
	}
}
