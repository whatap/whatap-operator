package v2alpha1

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestKubeVirtProductionHandlerRendersPrivateBootstrapOverHTTP(t *testing.T) {
	_, req, c := kubeVirtHandlerFixture(t)
	h := newKubeVirtAdmissionHandler(c, c)
	review := admissionv1.AdmissionReview{TypeMeta: metav1.TypeMeta{APIVersion: "admission.k8s.io/v1", Kind: "AdmissionReview"}, Request: &req.AdmissionRequest}
	body, err := json.Marshal(review)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "/whatap-injection-kubevirt-v1-virtualmachine", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	(&admission.Webhook{Handler: h}).ServeHTTP(recorder, request)
	var response admissionv1.AdmissionReview
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Response == nil || !response.Response.Allowed || response.Response.UID != req.UID {
		t.Fatal("production admission HTTP path did not produce an allowed correlated response")
	}
	if strings.Contains(string(response.Response.Patch), "test-license-not-real") {
		t.Fatal("license appeared in AdmissionReview response")
	}
	secrets := &corev1.SecretList{}
	if err := c.List(context.Background(), secrets, client.InNamespace("vms")); err != nil {
		t.Fatal(err)
	}
	var generated []byte
	for _, secret := range secrets.Items {
		if secret.Labels[bootstrapManagedLabel] == "true" {
			generated = secret.Data["userdata"]
		}
	}
	var config map[string]interface{}
	if err := yaml.Unmarshal(generated, &config); err != nil {
		t.Fatal("invalid generated cloud-config")
	}
	if config["timezone"] != "Asia/Seoul" {
		t.Fatal("original config was not preserved by production renderer")
	}
	files, ok := config["write_files"].([]interface{})
	if !ok || len(files) != 2 {
		t.Fatal("real bootstrap script and connection file were not generated")
	}
	if !strings.Contains(files[1].(map[string]interface{})["content"].(string), "license=test-license-not-real\n") {
		t.Fatal("license was not carried only in private file content")
	}
	if !strings.Contains(files[0].(map[string]interface{})["content"].(string), "systemctl start whatap-infra.service") {
		t.Fatal("production bootstrap lacks service start")
	}
}
