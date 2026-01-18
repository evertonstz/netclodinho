package k8s

import (
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Sandbox CRD GVR
var SandboxGVR = schema.GroupVersionResource{
	Group:    "agents.x-k8s.io",
	Version:  "v1alpha1",
	Resource: "sandboxes",
}

// SandboxGVK is the GroupVersionKind for Sandbox
var SandboxGVK = schema.GroupVersionKind{
	Group:   "agents.x-k8s.io",
	Version: "v1alpha1",
	Kind:    "Sandbox",
}

// Sandbox represents the Sandbox CRD
type Sandbox struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              SandboxSpec   `json:"spec,omitempty"`
	Status            SandboxStatus `json:"status,omitempty"`
}

// SandboxSpec defines the desired state
type SandboxSpec struct {
	PodTemplate          PodTemplateSpec `json:"podTemplate,omitempty"`
	VolumeClaimTemplates []PVCTemplate   `json:"volumeClaimTemplates,omitempty"`
}

// PodTemplateSpec is a simplified pod template
type PodTemplateSpec struct {
	Metadata metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec     PodSpec           `json:"spec,omitempty"`
}

// PodSpec is a simplified pod spec
type PodSpec struct {
	RuntimeClassName string      `json:"runtimeClassName,omitempty"`
	Containers       []Container `json:"containers,omitempty"`
}

// Container is a simplified container spec
type Container struct {
	Name            string           `json:"name"`
	Image           string           `json:"image"`
	Ports           []Port           `json:"ports,omitempty"`
	Env             []EnvVar         `json:"env,omitempty"`
	EnvFrom         []EnvFromSource  `json:"envFrom,omitempty"`
	VolumeMounts    []VolumeMount    `json:"volumeMounts,omitempty"`
	SecurityContext *SecurityContext `json:"securityContext,omitempty"`
	ReadinessProbe  *Probe           `json:"readinessProbe,omitempty"`
}

// Port defines a container port
type Port struct {
	ContainerPort int    `json:"containerPort"`
	Name          string `json:"name,omitempty"`
}

// EnvVar defines an environment variable
type EnvVar struct {
	Name  string `json:"name"`
	Value string `json:"value,omitempty"`
}

// EnvFromSource defines a source for environment variables
type EnvFromSource struct {
	SecretRef *SecretRef `json:"secretRef,omitempty"`
}

// SecretRef references a secret
type SecretRef struct {
	Name string `json:"name"`
}

// VolumeMount defines a volume mount
type VolumeMount struct {
	Name      string `json:"name"`
	MountPath string `json:"mountPath"`
}

// SecurityContext defines security options
type SecurityContext struct {
	Privileged             bool `json:"privileged,omitempty"`
	ReadOnlyRootFilesystem bool `json:"readOnlyRootFilesystem,omitempty"`
}

// Probe defines a health probe
type Probe struct {
	HTTPGet             *HTTPGetAction `json:"httpGet,omitempty"`
	InitialDelaySeconds int            `json:"initialDelaySeconds,omitempty"`
	PeriodSeconds       int            `json:"periodSeconds,omitempty"`
}

// HTTPGetAction defines an HTTP GET probe
type HTTPGetAction struct {
	Path string `json:"path"`
	Port int    `json:"port"`
}

// PVCTemplate defines a PVC template
type PVCTemplate struct {
	Metadata metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec     PVCSpec           `json:"spec,omitempty"`
}

// PVCSpec defines PVC spec
type PVCSpec struct {
	AccessModes      []string             `json:"accessModes,omitempty"`
	StorageClassName string               `json:"storageClassName,omitempty"`
	Resources        ResourceRequirements `json:"resources,omitempty"`
}

// ResourceRequirements defines resource requests
type ResourceRequirements struct {
	Requests map[string]string `json:"requests,omitempty"`
}

// SandboxStatus defines the observed state
type SandboxStatus struct {
	ServiceFQDN string             `json:"serviceFQDN,omitempty"`
	Conditions  []SandboxCondition `json:"conditions,omitempty"`
}

// SandboxCondition defines a condition
type SandboxCondition struct {
	Type               string      `json:"type"`
	Status             string      `json:"status"`
	Reason             string      `json:"reason,omitempty"`
	Message            string      `json:"message,omitempty"`
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
}

// SandboxList is a list of Sandbox resources
type SandboxList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Sandbox `json:"items"`
}

// DeepCopyObject implements runtime.Object
func (s *Sandbox) DeepCopyObject() runtime.Object {
	return s.DeepCopy()
}

// DeepCopy creates a deep copy
func (s *Sandbox) DeepCopy() *Sandbox {
	if s == nil {
		return nil
	}
	out := new(Sandbox)
	*out = *s
	out.ObjectMeta = *s.ObjectMeta.DeepCopy()
	return out
}

// DeepCopyObject implements runtime.Object
func (s *SandboxList) DeepCopyObject() runtime.Object {
	return s.DeepCopy()
}

// DeepCopy creates a deep copy
func (s *SandboxList) DeepCopy() *SandboxList {
	if s == nil {
		return nil
	}
	out := new(SandboxList)
	*out = *s
	out.ListMeta = *s.ListMeta.DeepCopy()
	if s.Items != nil {
		out.Items = make([]Sandbox, len(s.Items))
		for i := range s.Items {
			out.Items[i] = *s.Items[i].DeepCopy()
		}
	}
	return out
}

// GetObjectKind implements runtime.Object
func (s *Sandbox) GetObjectKind() schema.ObjectKind {
	return &s.TypeMeta
}

// GetObjectKind implements runtime.Object
func (s *SandboxList) GetObjectKind() schema.ObjectKind {
	return &s.TypeMeta
}

// SandboxStatusInfo is returned by GetStatus
type SandboxStatusInfo struct {
	Exists      bool
	Ready       bool
	ServiceFQDN string
	Error       string
}

// SandboxInfo contains basic information about a sandbox
type SandboxInfo struct {
	SessionID   string
	ServiceFQDN string
	Ready       bool
}

// IsReady returns true if the sandbox is ready
func (s *Sandbox) IsReady() bool {
	for _, c := range s.Status.Conditions {
		if c.Type == "Ready" {
			return c.Status == "True"
		}
	}
	return false
}

// GetError returns the error message only for actual failures, not transient states
func (s *Sandbox) GetError() string {
	for _, c := range s.Status.Conditions {
		if c.Type == "Ready" && c.Status == "False" {
			// Don't treat pending/creating states as errors
			if c.Reason == "DependenciesPending" || c.Reason == "PodPending" || c.Reason == "Creating" {
				return ""
			}
			// Check if message indicates a transient state (pod still starting up)
			if strings.Contains(c.Message, "phase: Pending") ||
				strings.Contains(c.Message, "phase: ContainerCreating") ||
				strings.Contains(c.Message, "Running but not Ready") ||
				strings.Contains(c.Message, "the object has been modified") {
				return ""
			}
			return c.Message
		}
	}
	return ""
}

// SandboxClaim CRD GVR (extensions API group)
var SandboxClaimGVR = schema.GroupVersionResource{
	Group:    "extensions.agents.x-k8s.io",
	Version:  "v1alpha1",
	Resource: "sandboxclaims",
}

// SandboxClaim represents a claim for a sandbox from the warm pool
type SandboxClaim struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              SandboxClaimSpec   `json:"spec,omitempty"`
	Status            SandboxClaimStatus `json:"status,omitempty"`
}

// SandboxClaimSpec defines the desired state of a SandboxClaim
type SandboxClaimSpec struct {
	SandboxTemplateRef SandboxTemplateRef `json:"sandboxTemplateRef"`
}

// SandboxTemplateRef references a SandboxTemplate
type SandboxTemplateRef struct {
	Name string `json:"name"`
}

// SandboxClaimStatus defines the observed state of a SandboxClaim
type SandboxClaimStatus struct {
	Conditions []SandboxCondition `json:"conditions,omitempty"`
	Sandbox    *SandboxReference  `json:"sandbox,omitempty"`
}

// SandboxReference references an assigned Sandbox
// Note: CRD uses capital "Name" field
type SandboxReference struct {
	Name string `json:"Name"`
}

// SandboxClaimInfo contains basic information about a claim
type SandboxClaimInfo struct {
	SessionID   string
	Bound       bool
	SandboxName string
}

// IsBound returns true if the claim has a sandbox assigned
func (c *SandboxClaim) IsBound() bool {
	return c.Status.Sandbox != nil && c.Status.Sandbox.Name != ""
}

// GetBoundSandboxName returns the name of the bound sandbox, or empty string
func (c *SandboxClaim) GetBoundSandboxName() string {
	if c.Status.Sandbox == nil {
		return ""
	}
	return c.Status.Sandbox.Name
}

// GetError returns the error message from claim conditions
func (c *SandboxClaim) GetError() string {
	for _, cond := range c.Status.Conditions {
		if cond.Type == "Ready" && cond.Status == "False" {
			return cond.Message
		}
	}
	return ""
}
