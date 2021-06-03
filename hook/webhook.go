package hook

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/golang/glog"
	"k8s.io/api/admission/v1beta1"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

var (
	runtimeScheme = runtime.NewScheme()
	codecs        = serializer.NewCodecFactory(runtimeScheme)
	deserializer  = codecs.UniversalDeserializer()
)

var (
	// can ignore namespaces
	ignoredNamespaces = []string{
		metav1.NamespaceSystem,
		metav1.NamespacePublic,
	}
	// config spec securityContext value
	securityContextValue int64 = 0
)

const (
	// if the object has the Annotation key and it's
	// value = "n" or "no" or "false" or "off", then do not mutate
	admissionWebhookAnnotationMutateKey = "cdp-patch-admission.datacreating.com/mutate"

	// mark the object has already mutated by  webhookserver
	// if this' value is "mutated", then do not continue mutate
	admissionWebhookAnnotationStatusKey = "cdp-patch-admission.datacreating.com/status"

	// admissionWebhookAnnotationStatusKey's value
	admissionWebhookAnnotationStatusKeyValue = "mutated"
)

type WebhookServer struct {
	Server *http.Server
}

// Webhook Server parameters
type WhSvrParameters struct {
	Port           int    // webhook server port
	CertFile       string // path to the x509 certificate for https
	KeyFile        string // path to the x509 private key matching `CertFile`
	sidecarCfgFile string // path to sidecar injector configuration file
}

type patchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

func init() {
	_ = appsv1.AddToScheme(runtimeScheme)
	_ = batchv1.AddToScheme(runtimeScheme)
	_ = admissionregistrationv1beta1.AddToScheme(runtimeScheme)
}

// Serve method for webhook server to modify securityContext
func (whsvr *WebhookServer) ServerHandle(w http.ResponseWriter, r *http.Request) {
	var body []byte
	if r.Body != nil {
		if data, err := ioutil.ReadAll(r.Body); err == nil {
			body = data
		}
	}
	if len(body) == 0 {
		glog.Error("empty body")
		http.Error(w, "empty body", http.StatusBadRequest)
		return
	}

	// verify the content type is accurate
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		glog.Errorf("Content-Type=%s, expect application/json", contentType)
		http.Error(w, "invalid Content-Type, expect `application/json`", http.StatusUnsupportedMediaType)
		return
	}
	// note request args
	glog.Infof("request to webhook for path=%v", r.URL.Path)

	var admissionResponse *v1beta1.AdmissionResponse
	ar := v1beta1.AdmissionReview{}
	if _, _, err := deserializer.Decode(body, nil, &ar); err != nil {
		glog.Errorf("Can't decode body: %v", err)
		admissionResponse = &v1beta1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
	} else {
		if r.URL.Path == "/mutate" {
			admissionResponse = whsvr.mutate(&ar)
		}
	}

	admissionReview := v1beta1.AdmissionReview{}
	if admissionResponse != nil {
		admissionReview.Response = admissionResponse
		if ar.Request != nil {
			admissionReview.Response.UID = ar.Request.UID
		}
	}

	resp, err := json.Marshal(admissionReview)
	if err != nil {
		glog.Errorf("Can't encode response: %v", err)
		http.Error(w, fmt.Sprintf("could not encode response: %v", err), http.StatusInternalServerError)
	}
	glog.Infof("Ready to write reponse ...")
	if _, err := w.Write(resp); err != nil {
		glog.Errorf("Can't write response: %v", err)
		http.Error(w, fmt.Sprintf("could not write response: %v", err), http.StatusInternalServerError)
	}
}

// main mutation process
func (whsvr *WebhookServer) mutate(ar *v1beta1.AdmissionReview) *v1beta1.AdmissionResponse {
	req := ar.Request
	var (
		availableAnnotations            map[string]string
		objectMeta                      *metav1.ObjectMeta
		resourceNamespace, resourceName string
	)

	var (
		deployment  appsv1.Deployment
		statefulset appsv1.StatefulSet
		job         batchv1.Job
	)
	if req.Kind.Kind == "Deployment" {
		if err := json.Unmarshal(req.Object.Raw, &deployment); err != nil {
			glog.Errorf("Could not unmarshal raw object: %v", err)
			return &v1beta1.AdmissionResponse{
				Result: &metav1.Status{
					Message: err.Error(),
				},
			}
		}
		resourceName, resourceNamespace, objectMeta = deployment.Name, deployment.Namespace, &deployment.ObjectMeta
	} else if req.Kind.Kind == "StatefulSet" {
		if err := json.Unmarshal(req.Object.Raw, &statefulset); err != nil {
			glog.Errorf("Could not unmarshal raw object: %v", err)
			return &v1beta1.AdmissionResponse{
				Result: &metav1.Status{
					Message: err.Error(),
				},
			}
		}
		resourceName, resourceNamespace, objectMeta = statefulset.Name, statefulset.Namespace, &statefulset.ObjectMeta
	} else if req.Kind.Kind == "Job" {
		if err := json.Unmarshal(req.Object.Raw, &job); err != nil {
			glog.Errorf("Could not unmarshal raw object: %v", err)
			return &v1beta1.AdmissionResponse{
				Result: &metav1.Status{
					Message: err.Error(),
				},
			}
		}
		resourceName, resourceNamespace, objectMeta = job.Name, job.Namespace, &job.ObjectMeta
	} else {
		glog.Infof("Skipping mutate for %s/%s,the resourse kind is %s, due to the kind is not Deployment or StatefulSet or Job", resourceNamespace, resourceName, req.Kind.Kind)
		return &v1beta1.AdmissionResponse{
			Allowed: true,
		}
	}

	glog.Infof("AdmissionReview for Kind=%v, Namespace=%v Name=%v (%v) UID=%v patchOperation=%v UserInfo=%v",
		req.Kind, req.Namespace, req.Name, resourceName, req.UID, req.Operation, req.UserInfo)
	// if can mutate
	if !mutationRequired(ignoredNamespaces, objectMeta) {
		glog.Infof("Skipping mutate for %s/%s due to policy check", resourceNamespace, resourceName)
		return &v1beta1.AdmissionResponse{
			Allowed: true,
		}
	}

	annotations := map[string]string{admissionWebhookAnnotationStatusKey: admissionWebhookAnnotationStatusKeyValue}
	// jiexun modify pod
	var (
		patchBytes []byte
		err        error
	)
	if req.Kind.Kind == "Deployment" {
		patchBytes, err = createRuntimeObjectAddContextPatch(&deployment, availableAnnotations, annotations)
	} else if req.Kind.Kind == "StatefulSet" {
		patchBytes, err = createRuntimeObjectAddContextPatch(&statefulset, availableAnnotations, annotations)
	} else {
		patchBytes, err = createRuntimeObjectAddContextPatch(&job, availableAnnotations, annotations)
	}

	if err != nil {
		return &v1beta1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
	}

	glog.Infof("AdmissionResponse: patch=%v\n", string(patchBytes))
	return &v1beta1.AdmissionResponse{
		Allowed: true,
		Patch:   patchBytes,
		PatchType: func() *v1beta1.PatchType {
			pt := v1beta1.PatchTypeJSONPatch
			return &pt
		}(),
	}
}

func admissionRequired(ignoredList []string, admissionAnnotationKey string, metadata *metav1.ObjectMeta) bool {
	// skip special kubernetes system namespaces
	for _, namespace := range ignoredList {
		if metadata.Namespace == namespace {
			glog.Infof("Skip validation for %v for it's in special namespace:%v", metadata.Name, metadata.Namespace)
			return false
		}
	}

	annotations := metadata.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}

	var required bool
	switch strings.ToLower(annotations[admissionAnnotationKey]) {
	default:
		required = true
	case "n", "no", "false", "off":
		required = false
	}
	return required
}

func mutationRequired(ignoredList []string, metadata *metav1.ObjectMeta) bool {
	required := admissionRequired(ignoredList, admissionWebhookAnnotationMutateKey, metadata)
	annotations := metadata.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	status := annotations[admissionWebhookAnnotationStatusKey]

	if strings.ToLower(status) == admissionWebhookAnnotationStatusKeyValue {
		required = false
	}

	glog.Infof("Mutation policy for %v/%v: required:%v", metadata.Namespace, metadata.Name, required)
	return required
}
