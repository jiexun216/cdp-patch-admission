package hook

import (
	"context"
	"encoding/json"
	"k8s.io/apimachinery/pkg/runtime"
	"os"
	"strconv"
	"strings"

	"github.com/golang/glog"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)



// pod level securityContext
func patchPodSecurityContext(patch *[]patchOperation, podSecurityContext *corev1.PodSecurityContext)  {
	if podSecurityContext != nil {
		// modify /spec/template/spec/securityContext
		replaceSecurityContext := patchOperation{
			Op:    "replace",
			Path:  "/spec/template/spec/securityContext",
			Value: podSecurityContext,
		}
		glog.Infof("add  securityContext  /spec/template/spec/securityContext for value: %v", replaceSecurityContext)
		*patch = append(*patch, replaceSecurityContext)
	}
}

// pod level Volumes
func patchPodVolumes(patch *[]patchOperation, preVolume []corev1.Volume, appendVolumes []corev1.Volume)  {
	if len(appendVolumes) > 0 {
		// modify /spec/template/spec/volumes
		replaceVolumes := patchOperation{
			Op:    "replace",
			Path:  "/spec/template/spec/volumes",
			Value: append(preVolume, appendVolumes...),
		}
		glog.Infof("add  Volumes  /spec/template/spec/Volumes for value: %v", replaceVolumes)
		*patch = append(*patch, replaceVolumes)
	}
}

// pod level affinity
func patchPodAffinity(patch *[]patchOperation, affinity *corev1.Affinity) {
	if affinity != nil {
		if affinity.PodAntiAffinity != nil {
			// modify /spec/template/spec/affinity/podAntiAffinity
			replacePodAntiAffinity := patchOperation{
				Op:    "replace",
				Path:  "/spec/template/spec/affinity/podAntiAffinity",
				Value: affinity.PodAntiAffinity,
			}
			glog.Infof("add  Affinity podAntiAffinity  /spec/template/spec/affinity/podAntiAffinity for value: %v", replacePodAntiAffinity)
			*patch = append(*patch, replacePodAntiAffinity)
		}
		if affinity.PodAffinity != nil {
			// modify /spec/template/spec/affinity/podAffinity
			replacePodAffinity := patchOperation{
				Op:    "replace",
				Path:  "/spec/template/spec/affinity/podAffinity",
				Value: affinity.PodAffinity,
			}
			glog.Infof("add Affinity PodAffinity  /spec/template/spec/affinity/podAffinity for value: %v", replacePodAffinity)
			*patch = append(*patch, replacePodAffinity)
		}
		if affinity.NodeAffinity != nil {
			// modify /spec/template/spec/affinity/nodeAffinity
			replaceNodeAffinity := patchOperation{
				Op:    "replace",
				Path:  "/spec/template/spec/affinity/nodeAffinity",
				Value: affinity.NodeAffinity,
			}
			glog.Infof("add Affinity NodeAffinity /spec/template/spec/affinity/nodeAffinity for value: %v", replaceNodeAffinity)
			*patch = append(*patch, replaceNodeAffinity)
		}
	}
}

// initContainers level securityContext volumeMounts
func patchInitContainers(patch *[]patchOperation, templateInitContainers []corev1.Container, preInitContainers []corev1.Container)  {
	if len(templateInitContainers) > 0 {
		for i := 0; i < len(preInitContainers); i++ {
			for _, val := range templateInitContainers {
				if val.Name == preInitContainers[i].Name {
					// only add securityContext initcontainers
					if val.SecurityContext != nil {
						replaceSecurityContext := patchOperation{
							Op:    "replace",
							Path:  "/spec/template/spec/initContainers/" + strconv.Itoa(i) + "/securityContext",
							Value: val.SecurityContext,
						}
						*patch = append(*patch, replaceSecurityContext)
					}
					// only add Volumes initcontainers
					if len(val.VolumeMounts) > 0 {
						replaceVolumeMounts := patchOperation{
							Op:    "replace",
							Path:  "/spec/template/spec/initContainers/" + strconv.Itoa(i) + "/volumeMounts",
							Value: append(preInitContainers[i].VolumeMounts, val.VolumeMounts...),
						}
						*patch = append(*patch, replaceVolumeMounts)
					}
				}
			}
		}
	}
}


// containers level securityContext volumeMounts
func patchContainers(patch *[]patchOperation, templateContainers []corev1.Container, preContainers []corev1.Container) {
	if len(templateContainers) > 0 {
		for i := 0; i < len(preContainers); i++ {
			for _, val := range templateContainers {
				if val.Name == preContainers[i].Name {
					// only add securityContext containers
					if val.SecurityContext != nil {
						replaceSecurityContext := patchOperation{
							Op:    "replace",
							Path:  "/spec/template/spec/containers/" + strconv.Itoa(i) + "/securityContext",
							Value: val.SecurityContext,
						}
						*patch = append(*patch, replaceSecurityContext)
					}

					// only add volumeMounts containers
					if len(val.VolumeMounts) > 0 {
						replaceVolumeMounts := patchOperation{
							Op:    "replace",
							Path:  "/spec/template/spec/containers/" + strconv.Itoa(i) + "/volumeMounts",
							Value: append(preContainers[i].VolumeMounts, val.VolumeMounts...),
						}
						*patch = append(*patch, replaceVolumeMounts)
					}

				}
			}
		}
	}
}


func createRuntimeObjectAddContextPatch(object runtime.Object, availableAnnotations map[string]string, annotations map[string]string) ([]byte, error) {
	var patch []patchOperation
	// update Annotation to set admissionWebhookAnnotationStatusKey: "mutated"
	patch = append(patch, updateAnnotation(availableAnnotations, annotations)...)

	// read configMap to decide modify the sts
	securitycontextMap := getConfigMap()
	if securitycontextMap == nil {
		return json.Marshal(patch)
	}

	var mapKeyName, objectNamespace string
	var preVolumes []corev1.Volume
	var initContainers, containers []corev1.Container

	switch initObject := object.(type) {
	case *appsv1.Deployment:
		mapKeyName = "deployment." + initObject.Name
		objectNamespace = initObject.Namespace
		preVolumes = initObject.Spec.Template.Spec.Volumes
		initContainers = initObject.Spec.Template.Spec.InitContainers
		containers = initObject.Spec.Template.Spec.Containers

	case *appsv1.StatefulSet:
		mapKeyName = "statefulset." + initObject.Name
		objectNamespace = initObject.Namespace
		preVolumes = initObject.Spec.Template.Spec.Volumes
		initContainers = initObject.Spec.Template.Spec.InitContainers
		containers = initObject.Spec.Template.Spec.Containers
	case *batchv1.Job:
		mapKeyName = "job." + initObject.Name
		objectNamespace = initObject.Namespace
		preVolumes = initObject.Spec.Template.Spec.Volumes
		initContainers = initObject.Spec.Template.Spec.InitContainers
		containers = initObject.Spec.Template.Spec.Containers
	}


	for k, value := range securitycontextMap {
		if strings.Contains(mapKeyName, k) {
			var specTemplate appsv1.Deployment
			if err := json.Unmarshal([]byte(strings.Replace(value, "{{namespace}}", objectNamespace, -1)), &specTemplate); err != nil {
				glog.Errorf("Can't json.Unmarshal deployTemplate: %v", err)
			}
			// pod level SecurityContext
			patchPodSecurityContext(&patch, specTemplate.Spec.Template.Spec.SecurityContext)
			// pod level Volumes
			patchPodVolumes(&patch, preVolumes, specTemplate.Spec.Template.Spec.Volumes)
			// pod level affinity
			patchPodAffinity(&patch, specTemplate.Spec.Template.Spec.Affinity)
			// initContainers level securityContext volumeMounts
			patchInitContainers(&patch, specTemplate.Spec.Template.Spec.InitContainers, initContainers)
			// containers level securityContext volumeMounts
			patchContainers(&patch, specTemplate.Spec.Template.Spec.Containers, containers)
		}
	}

	return json.Marshal(patch)
}


func updateAnnotation(target map[string]string, added map[string]string) (patch []patchOperation) {
	for key, value := range added {
		if target == nil || target[key] == "" {
			target = map[string]string{}
			patch = append(patch, patchOperation{
				Op:   "add",
				Path: "/metadata/annotations",
				Value: map[string]string{
					key: value,
				},
			})
		} else {
			patch = append(patch, patchOperation{
				Op:    "replace",
				Path:  "/metadata/annotations/" + key,
				Value: value,
			})
		}
	}
	return patch
}

func getConfigMap() map[string]string {
	config, err := rest.InClusterConfig()
	if err != nil {
		glog.Errorf("Can't get ClusterConfig: %v", err)
		return nil
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		glog.Errorf("Can't connect to kubernetes: %v", err)
		return nil
	}

	configMapClient := clientset.CoreV1().ConfigMaps("cdp-customizer")
	// labelSelector cdp.cloudera.io/patch
	labelSelectorKey := os.Getenv("CONFIGMAP_LABEL_SELECTOR_KEY")
	glog.Infof("get the env CONFIGMAP_LABEL_SELECTOR_KEY for value: %v", labelSelectorKey)
	labelSelector := metav1.LabelSelector{
		//MatchLabels: map[string]string{"cdp.cloudera.io/security-context":"true"},
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key: labelSelectorKey,
				Operator: "Exists",
			},
		},
	}
	listOptions := metav1.ListOptions{
		LabelSelector: labels.Set(labelSelector.MatchLabels).String(),
	}
	configMapList, err := configMapClient.List(context.Background(), listOptions)
	if err != nil {
		glog.Errorf("Can't get the specific configMap: %v", err)
		return nil
	}
	mergeResult := make(map[string]string)
	for _, configMap := range configMapList.Items {
		for k, v := range configMap.Data {
			mergeResult[k] = v
		}
	}
	if len(mergeResult) == 0 {
		glog.Errorf("get the specific configMap,but is empty")
		return nil
	}
	return mergeResult

}
