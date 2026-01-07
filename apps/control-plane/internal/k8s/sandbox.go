package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/angristan/netclode/apps/control-plane/internal/config"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

// SandboxReadyCallback is called when a sandbox becomes ready
type SandboxReadyCallback func(sessionID string, serviceFQDN string, err error)

// ClaimBoundCallback is called when a SandboxClaim is bound to a sandbox
type ClaimBoundCallback func(sessionID string, sandboxName string, err error)

type k8sRuntime struct {
	dynamicClient dynamic.Interface
	clientset     *kubernetes.Clientset
	namespace     string
	config        *config.Config

	// Informer for watching sandbox changes
	informer      cache.SharedIndexInformer
	informerStop  chan struct{}

	// Callbacks for sandbox ready notifications
	readyCallbacks map[string]SandboxReadyCallback
	callbacksMu    sync.RWMutex

	// Cache of sandbox states
	sandboxCache map[string]*Sandbox
	cacheMu      sync.RWMutex

	// SandboxClaim informer and cache (for warm pool mode)
	claimInformer  cache.SharedIndexInformer
	claimCallbacks map[string]ClaimBoundCallback
	claimCache     map[string]*SandboxClaim
}

func newK8sRuntime(cfg *config.Config) (*k8sRuntime, error) {
	restConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("get in-cluster config: %w", err)
	}

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create dynamic client: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create clientset: %w", err)
	}

	r := &k8sRuntime{
		dynamicClient:  dynamicClient,
		clientset:      clientset,
		namespace:      cfg.K8sNamespace,
		config:         cfg,
		informerStop:   make(chan struct{}),
		readyCallbacks: make(map[string]SandboxReadyCallback),
		sandboxCache:   make(map[string]*Sandbox),
		claimCallbacks: make(map[string]ClaimBoundCallback),
		claimCache:     make(map[string]*SandboxClaim),
	}

	// Setup sandbox informer
	if err := r.setupInformer(); err != nil {
		return nil, fmt.Errorf("setup sandbox informer: %w", err)
	}

	// Setup claim informer if warm pool is enabled
	if cfg.UseWarmPool {
		if err := r.setupClaimInformer(); err != nil {
			return nil, fmt.Errorf("setup claim informer: %w", err)
		}
	}

	slog.Info("Kubernetes client initialized with informer", "namespace", cfg.K8sNamespace, "warmPool", cfg.UseWarmPool)
	return r, nil
}

func (r *k8sRuntime) setupInformer() error {
	factory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(
		r.dynamicClient,
		30*time.Second, // Resync period
		r.namespace,
		func(opts *metav1.ListOptions) {
			opts.LabelSelector = "netclode.io/session"
		},
	)

	r.informer = factory.ForResource(SandboxGVR).Informer()

	_, err := r.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    r.onSandboxAdd,
		UpdateFunc: r.onSandboxUpdate,
		DeleteFunc: r.onSandboxDelete,
	})
	if err != nil {
		return err
	}

	// Start informer in background
	go r.informer.Run(r.informerStop)

	// Wait for initial sync
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if !cache.WaitForCacheSync(ctx.Done(), r.informer.HasSynced) {
		return fmt.Errorf("timeout waiting for informer sync")
	}

	slog.Info("Sandbox informer synced")
	return nil
}

func (r *k8sRuntime) onSandboxAdd(obj interface{}) {
	sandbox := r.unstructuredToSandbox(obj)
	if sandbox == nil {
		return
	}

	sessionID := r.getSessionID(sandbox)
	slog.Debug("Sandbox added", "sessionID", sessionID, "ready", sandbox.IsReady())

	r.cacheMu.Lock()
	r.sandboxCache[sessionID] = sandbox
	r.cacheMu.Unlock()

	r.checkAndNotify(sessionID, sandbox)
}

func (r *k8sRuntime) onSandboxUpdate(oldObj, newObj interface{}) {
	sandbox := r.unstructuredToSandbox(newObj)
	if sandbox == nil {
		return
	}

	sessionID := r.getSessionID(sandbox)
	slog.Debug("Sandbox updated", "sessionID", sessionID, "ready", sandbox.IsReady(), "fqdn", sandbox.Status.ServiceFQDN)

	r.cacheMu.Lock()
	r.sandboxCache[sessionID] = sandbox
	r.cacheMu.Unlock()

	r.checkAndNotify(sessionID, sandbox)
}

func (r *k8sRuntime) onSandboxDelete(obj interface{}) {
	sandbox := r.unstructuredToSandbox(obj)
	if sandbox == nil {
		// Handle DeletedFinalStateUnknown
		if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
			sandbox = r.unstructuredToSandbox(tombstone.Obj)
		}
	}
	if sandbox == nil {
		return
	}

	sessionID := r.getSessionID(sandbox)
	slog.Debug("Sandbox deleted", "sessionID", sessionID)

	r.cacheMu.Lock()
	delete(r.sandboxCache, sessionID)
	r.cacheMu.Unlock()
}

func (r *k8sRuntime) unstructuredToSandbox(obj interface{}) *Sandbox {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return nil
	}

	data, err := u.MarshalJSON()
	if err != nil {
		slog.Warn("Failed to marshal unstructured", "error", err)
		return nil
	}

	var sandbox Sandbox
	if err := json.Unmarshal(data, &sandbox); err != nil {
		slog.Warn("Failed to unmarshal sandbox", "error", err)
		return nil
	}

	return &sandbox
}

func (r *k8sRuntime) getSessionID(sandbox *Sandbox) string {
	if id, ok := sandbox.Labels["netclode.io/session"]; ok {
		return id
	}
	// Fallback: extract from name
	name := sandbox.Name
	if strings.HasPrefix(name, "sess-") {
		return strings.TrimPrefix(name, "sess-")
	}
	return ""
}

func (r *k8sRuntime) checkAndNotify(sessionID string, sandbox *Sandbox) {
	r.callbacksMu.RLock()
	callback, ok := r.readyCallbacks[sessionID]
	r.callbacksMu.RUnlock()

	if !ok {
		return
	}

	if sandbox.IsReady() {
		fqdn := sandbox.Status.ServiceFQDN
		// Construct FQDN if not set (warm pool controller doesn't populate it)
		if fqdn == "" {
			fqdn = fmt.Sprintf("%s.%s.svc.cluster.local", sandbox.Name, r.namespace)
		}

		// Remove callback before invoking to prevent double-call
		r.callbacksMu.Lock()
		delete(r.readyCallbacks, sessionID)
		r.callbacksMu.Unlock()

		callback(sessionID, fqdn, nil)
	} else if errMsg := sandbox.GetError(); errMsg != "" {
		r.callbacksMu.Lock()
		delete(r.readyCallbacks, sessionID)
		r.callbacksMu.Unlock()

		callback(sessionID, "", fmt.Errorf("sandbox error: %s", errMsg))
	}
}

// Close stops the informer
func (r *k8sRuntime) Close() {
	close(r.informerStop)
}

func sandboxName(sessionID string) string {
	return "sess-" + sessionID
}

func secretName(sessionID string) string {
	return "sess-" + sessionID + "-env"
}

func pvcName(sessionID string) string {
	return "workspace-sess-" + sessionID
}

// CreateSandbox creates a new sandbox for a session.
func (r *k8sRuntime) CreateSandbox(ctx context.Context, sessionID string, env map[string]string) error {
	// First create the environment secret
	if err := r.createEnvSecret(ctx, sessionID, env); err != nil {
		return fmt.Errorf("create env secret: %w", err)
	}

	// Create the Sandbox CRD
	sandbox := r.buildSandboxManifest(sessionID)

	data, err := json.Marshal(sandbox)
	if err != nil {
		_ = r.DeleteSecret(ctx, sessionID)
		return fmt.Errorf("marshal sandbox: %w", err)
	}

	var u unstructured.Unstructured
	if err := json.Unmarshal(data, &u.Object); err != nil {
		_ = r.DeleteSecret(ctx, sessionID)
		return fmt.Errorf("convert to unstructured: %w", err)
	}

	_, err = r.dynamicClient.Resource(SandboxGVR).Namespace(r.namespace).Create(ctx, &u, metav1.CreateOptions{})
	if err != nil {
		_ = r.DeleteSecret(ctx, sessionID)
		return fmt.Errorf("create sandbox: %w", err)
	}

	slog.Info("Sandbox created", "sessionID", sessionID, "name", sandboxName(sessionID))
	return nil
}

func (r *k8sRuntime) createEnvSecret(ctx context.Context, sessionID string, env map[string]string) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName(sessionID),
			Namespace: r.namespace,
			Labels: map[string]string{
				"netclode.io/session": sessionID,
			},
		},
		Type:       corev1.SecretTypeOpaque,
		StringData: env,
	}

	_, err := r.clientset.CoreV1().Secrets(r.namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func (r *k8sRuntime) buildSandboxManifest(sessionID string) *Sandbox {
	name := sandboxName(sessionID)

	return &Sandbox{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "agents.x-k8s.io/v1alpha1",
			Kind:       "Sandbox",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: r.namespace,
			Labels: map[string]string{
				"netclode.io/session": sessionID,
			},
		},
		Spec: SandboxSpec{
			PodTemplate: PodTemplateSpec{
				Spec: PodSpec{
					RuntimeClassName: "kata-clh",
					Containers: []Container{
						{
							Name:  "agent",
							Image: r.config.AgentImage,
							SecurityContext: &SecurityContext{
								Privileged:             true,
								ReadOnlyRootFilesystem: false,
							},
							Ports: []Port{
								{ContainerPort: 3002, Name: "http"},
							},
							Env: []EnvVar{
								{Name: "NODE_ENV", Value: "production"},
								{Name: "WORKSPACE", Value: "/workspace"},
							},
							EnvFrom: []EnvFromSource{
								{SecretRef: &SecretRef{Name: secretName(sessionID)}},
							},
							VolumeMounts: []VolumeMount{
								{Name: "workspace", MountPath: "/workspace"},
							},
							ReadinessProbe: &Probe{
								HTTPGet: &HTTPGetAction{
									Path: "/health",
									Port: 3002,
								},
								InitialDelaySeconds: 3,
								PeriodSeconds:       2,
							},
						},
					},
				},
			},
			VolumeClaimTemplates: []PVCTemplate{
				{
					Metadata: metav1.ObjectMeta{
						Name: "workspace",
						Labels: map[string]string{
							"netclode.io/session": sessionID,
						},
					},
					Spec: PVCSpec{
						AccessModes:      []string{"ReadWriteOnce"},
						StorageClassName: "juicefs-sc",
						Resources: ResourceRequirements{
							Requests: map[string]string{
								"storage": "10Gi",
							},
						},
					},
				},
			},
		},
	}
}

// WaitForReady registers a callback to be called when sandbox becomes ready.
// Uses informer-based watching instead of polling.
func (r *k8sRuntime) WaitForReady(ctx context.Context, sessionID string, timeout time.Duration) (string, error) {
	// Check if already ready from cache
	r.cacheMu.RLock()
	sandbox, exists := r.sandboxCache[sessionID]
	r.cacheMu.RUnlock()

	if exists && sandbox.IsReady() {
		fqdn := sandbox.Status.ServiceFQDN
		// Construct FQDN if not set (warm pool controller doesn't populate it)
		if fqdn == "" {
			fqdn = fmt.Sprintf("%s.%s.svc.cluster.local", sandbox.Name, r.namespace)
		}
		return fqdn, nil
	}

	// Setup callback channel
	resultCh := make(chan struct {
		fqdn string
		err  error
	}, 1)

	r.callbacksMu.Lock()
	r.readyCallbacks[sessionID] = func(sid string, fqdn string, err error) {
		resultCh <- struct {
			fqdn string
			err  error
		}{fqdn, err}
	}
	r.callbacksMu.Unlock()

	// Cleanup callback on exit
	defer func() {
		r.callbacksMu.Lock()
		delete(r.readyCallbacks, sessionID)
		r.callbacksMu.Unlock()
	}()

	// Wait for result or timeout
	select {
	case result := <-resultCh:
		if result.err != nil {
			return "", result.err
		}
		slog.Info("Sandbox ready (via informer)", "sessionID", sessionID, "serviceFQDN", result.fqdn)
		return result.fqdn, nil
	case <-time.After(timeout):
		return "", fmt.Errorf("timeout waiting for sandbox %s to be ready", sandboxName(sessionID))
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// WatchSandboxReady registers a callback without blocking.
// The callback will be called when the sandbox becomes ready or errors.
func (r *k8sRuntime) WatchSandboxReady(sessionID string, callback SandboxReadyCallback) {
	// Check if already ready from cache
	r.cacheMu.RLock()
	sandbox, exists := r.sandboxCache[sessionID]
	r.cacheMu.RUnlock()

	if exists && sandbox.IsReady() {
		fqdn := sandbox.Status.ServiceFQDN
		// Construct FQDN if not set (warm pool controller doesn't populate it)
		if fqdn == "" {
			fqdn = fmt.Sprintf("%s.%s.svc.cluster.local", sandbox.Name, r.namespace)
		}
		go callback(sessionID, fqdn, nil)
		return
	}

	if exists {
		if errMsg := sandbox.GetError(); errMsg != "" {
			go callback(sessionID, "", fmt.Errorf("sandbox error: %s", errMsg))
			return
		}
	}

	// Register callback for future updates
	r.callbacksMu.Lock()
	r.readyCallbacks[sessionID] = callback
	r.callbacksMu.Unlock()
}

// GetStatus retrieves the status of a sandbox from cache.
func (r *k8sRuntime) GetStatus(ctx context.Context, sessionID string) (*SandboxStatusInfo, error) {
	r.cacheMu.RLock()
	sandbox, exists := r.sandboxCache[sessionID]
	r.cacheMu.RUnlock()

	if !exists {
		// Try fetching directly
		name := sandboxName(sessionID)
		u, err := r.dynamicClient.Resource(SandboxGVR).Namespace(r.namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				return &SandboxStatusInfo{Exists: false}, nil
			}
			return nil, err
		}
		sandbox = r.unstructuredToSandbox(u)
		if sandbox == nil {
			return &SandboxStatusInfo{Exists: false}, nil
		}
	}

	return &SandboxStatusInfo{
		Exists:      true,
		Ready:       sandbox.IsReady(),
		ServiceFQDN: sandbox.Status.ServiceFQDN,
		Error:       sandbox.GetError(),
	}, nil
}

// DeleteSandbox deletes a sandbox.
func (r *k8sRuntime) DeleteSandbox(ctx context.Context, sessionID string) error {
	name := sandboxName(sessionID)

	err := r.dynamicClient.Resource(SandboxGVR).Namespace(r.namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	slog.Info("Sandbox deleted", "sessionID", sessionID, "name", name)
	return nil
}

// DeletePVC deletes the persistent volume claim for a session.
func (r *k8sRuntime) DeletePVC(ctx context.Context, sessionID string) error {
	name := pvcName(sessionID)

	err := r.clientset.CoreV1().PersistentVolumeClaims(r.namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	slog.Info("PVC deleted", "sessionID", sessionID, "name", name)
	return nil
}

// DeleteSecret deletes the environment secret for a session.
func (r *k8sRuntime) DeleteSecret(ctx context.Context, sessionID string) error {
	name := secretName(sessionID)

	err := r.clientset.CoreV1().Secrets(r.namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	slog.Info("Secret deleted", "sessionID", sessionID, "name", name)
	return nil
}

// ListSandboxes lists all sandboxes from cache.
func (r *k8sRuntime) ListSandboxes(ctx context.Context) ([]SandboxInfo, error) {
	r.cacheMu.RLock()
	defer r.cacheMu.RUnlock()

	sandboxes := make([]SandboxInfo, 0, len(r.sandboxCache))
	for sessionID, sandbox := range r.sandboxCache {
		sandboxes = append(sandboxes, SandboxInfo{
			SessionID:   sessionID,
			ServiceFQDN: sandbox.Status.ServiceFQDN,
			Ready:       sandbox.IsReady(),
		})
	}

	return sandboxes, nil
}

// ============================================================================
// SandboxClaim operations (warm pool mode)
// ============================================================================

func sandboxClaimName(sessionID string) string {
	return "sess-" + sessionID
}

func (r *k8sRuntime) setupClaimInformer() error {
	factory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(
		r.dynamicClient,
		30*time.Second,
		r.namespace,
		func(opts *metav1.ListOptions) {
			opts.LabelSelector = "netclode.io/session"
		},
	)

	r.claimInformer = factory.ForResource(SandboxClaimGVR).Informer()

	_, err := r.claimInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    r.onClaimAdd,
		UpdateFunc: r.onClaimUpdate,
		DeleteFunc: r.onClaimDelete,
	})
	if err != nil {
		return err
	}

	// Start informer in background
	go r.claimInformer.Run(r.informerStop)

	// Wait for initial sync
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if !cache.WaitForCacheSync(ctx.Done(), r.claimInformer.HasSynced) {
		return fmt.Errorf("timeout waiting for claim informer sync")
	}

	slog.Info("SandboxClaim informer synced")
	return nil
}

func (r *k8sRuntime) onClaimAdd(obj interface{}) {
	claim := r.unstructuredToClaim(obj)
	if claim == nil {
		return
	}

	sessionID := r.getSessionIDFromClaim(claim)
	slog.Debug("SandboxClaim added", "sessionID", sessionID, "bound", claim.IsBound())

	r.cacheMu.Lock()
	r.claimCache[sessionID] = claim
	r.cacheMu.Unlock()

	r.checkAndNotifyClaim(sessionID, claim)
}

func (r *k8sRuntime) onClaimUpdate(oldObj, newObj interface{}) {
	claim := r.unstructuredToClaim(newObj)
	if claim == nil {
		return
	}

	sessionID := r.getSessionIDFromClaim(claim)
	sandboxName := claim.GetBoundSandboxName()
	slog.Debug("SandboxClaim updated", "sessionID", sessionID, "bound", claim.IsBound(), "sandbox", sandboxName)

	r.cacheMu.Lock()
	r.claimCache[sessionID] = claim
	r.cacheMu.Unlock()

	r.checkAndNotifyClaim(sessionID, claim)
}

func (r *k8sRuntime) onClaimDelete(obj interface{}) {
	claim := r.unstructuredToClaim(obj)
	if claim == nil {
		if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
			claim = r.unstructuredToClaim(tombstone.Obj)
		}
	}
	if claim == nil {
		return
	}

	sessionID := r.getSessionIDFromClaim(claim)
	slog.Debug("SandboxClaim deleted", "sessionID", sessionID)

	r.cacheMu.Lock()
	delete(r.claimCache, sessionID)
	r.cacheMu.Unlock()
}

func (r *k8sRuntime) unstructuredToClaim(obj interface{}) *SandboxClaim {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return nil
	}

	data, err := u.MarshalJSON()
	if err != nil {
		slog.Warn("Failed to marshal unstructured claim", "error", err)
		return nil
	}

	var claim SandboxClaim
	if err := json.Unmarshal(data, &claim); err != nil {
		slog.Warn("Failed to unmarshal claim", "error", err)
		return nil
	}

	return &claim
}

func (r *k8sRuntime) getSessionIDFromClaim(claim *SandboxClaim) string {
	if id, ok := claim.Labels["netclode.io/session"]; ok {
		return id
	}
	// Fallback: extract from name
	name := claim.Name
	if strings.HasPrefix(name, "sess-") {
		return strings.TrimPrefix(name, "sess-")
	}
	return ""
}

func (r *k8sRuntime) checkAndNotifyClaim(sessionID string, claim *SandboxClaim) {
	r.callbacksMu.RLock()
	callback, ok := r.claimCallbacks[sessionID]
	r.callbacksMu.RUnlock()

	if !ok {
		return
	}

	if claim.IsBound() {
		// Remove callback before invoking to prevent double-call
		r.callbacksMu.Lock()
		delete(r.claimCallbacks, sessionID)
		r.callbacksMu.Unlock()

		callback(sessionID, claim.GetBoundSandboxName(), nil)
	} else if errMsg := claim.GetError(); errMsg != "" {
		r.callbacksMu.Lock()
		delete(r.claimCallbacks, sessionID)
		r.callbacksMu.Unlock()

		callback(sessionID, "", fmt.Errorf("claim error: %s", errMsg))
	}
}

// CreateSandboxClaim creates a claim to request a sandbox from the warm pool.
func (r *k8sRuntime) CreateSandboxClaim(ctx context.Context, sessionID string) error {
	claim := &SandboxClaim{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "extensions.agents.x-k8s.io/v1alpha1",
			Kind:       "SandboxClaim",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      sandboxClaimName(sessionID),
			Namespace: r.namespace,
			Labels: map[string]string{
				"netclode.io/session": sessionID,
			},
		},
		Spec: SandboxClaimSpec{
			SandboxTemplateRef: SandboxTemplateRef{
				Name: r.config.SandboxTemplate,
			},
		},
	}

	data, err := json.Marshal(claim)
	if err != nil {
		return fmt.Errorf("marshal claim: %w", err)
	}

	var u unstructured.Unstructured
	if err := json.Unmarshal(data, &u.Object); err != nil {
		return fmt.Errorf("convert to unstructured: %w", err)
	}

	_, err = r.dynamicClient.Resource(SandboxClaimGVR).Namespace(r.namespace).Create(ctx, &u, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("create claim: %w", err)
	}

	slog.Info("SandboxClaim created", "sessionID", sessionID, "template", r.config.SandboxTemplate)
	return nil
}

// WaitForClaimBound waits for a SandboxClaim to be bound to a sandbox.
func (r *k8sRuntime) WaitForClaimBound(ctx context.Context, sessionID string, timeout time.Duration) (string, error) {
	// Check if already bound from cache
	r.cacheMu.RLock()
	claim, exists := r.claimCache[sessionID]
	r.cacheMu.RUnlock()

	if exists && claim.IsBound() {
		return claim.GetBoundSandboxName(), nil
	}

	// Setup callback channel
	resultCh := make(chan struct {
		sandboxName string
		err         error
	}, 1)

	r.callbacksMu.Lock()
	r.claimCallbacks[sessionID] = func(sid, name string, err error) {
		resultCh <- struct {
			sandboxName string
			err         error
		}{name, err}
	}
	r.callbacksMu.Unlock()

	// Cleanup callback on exit
	defer func() {
		r.callbacksMu.Lock()
		delete(r.claimCallbacks, sessionID)
		r.callbacksMu.Unlock()
	}()

	// Wait for result or timeout
	select {
	case result := <-resultCh:
		if result.err != nil {
			return "", result.err
		}
		slog.Info("SandboxClaim bound", "sessionID", sessionID, "sandbox", result.sandboxName)
		return result.sandboxName, nil
	case <-time.After(timeout):
		return "", fmt.Errorf("timeout waiting for claim %s to be bound", sandboxClaimName(sessionID))
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// GetSandboxByName retrieves a sandbox by its name.
func (r *k8sRuntime) GetSandboxByName(ctx context.Context, name string) (*Sandbox, error) {
	u, err := r.dynamicClient.Resource(SandboxGVR).Namespace(r.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	sandbox := r.unstructuredToSandbox(u)
	if sandbox == nil {
		return nil, fmt.Errorf("failed to parse sandbox %s", name)
	}

	return sandbox, nil
}

// LabelSandbox adds the netclode.io/session label to a sandbox so the informer can track it.
func (r *k8sRuntime) LabelSandbox(ctx context.Context, sandboxName string, sessionID string) error {
	patch := []byte(fmt.Sprintf(`{"metadata":{"labels":{"netclode.io/session":"%s"}}}`, sessionID))

	_, err := r.dynamicClient.Resource(SandboxGVR).Namespace(r.namespace).Patch(
		ctx, sandboxName, types.MergePatchType, patch, metav1.PatchOptions{},
	)
	if err != nil {
		return fmt.Errorf("patch sandbox: %w", err)
	}

	slog.Info("Sandbox labeled", "sandbox", sandboxName, "sessionID", sessionID)
	return nil
}

// DeleteSandboxClaim deletes a SandboxClaim.
func (r *k8sRuntime) DeleteSandboxClaim(ctx context.Context, sessionID string) error {
	name := sandboxClaimName(sessionID)

	err := r.dynamicClient.Resource(SandboxClaimGVR).Namespace(r.namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	slog.Info("SandboxClaim deleted", "sessionID", sessionID, "name", name)
	return nil
}

// ListSandboxClaims lists all SandboxClaims from cache.
func (r *k8sRuntime) ListSandboxClaims(ctx context.Context) ([]SandboxClaimInfo, error) {
	r.cacheMu.RLock()
	defer r.cacheMu.RUnlock()

	claims := make([]SandboxClaimInfo, 0, len(r.claimCache))
	for sessionID, claim := range r.claimCache {
		claims = append(claims, SandboxClaimInfo{
			SessionID:   sessionID,
			Bound:       claim.IsBound(),
			SandboxName: claim.GetBoundSandboxName(),
		})
	}

	return claims, nil
}

// Ensure k8sRuntime implements Runtime
var _ Runtime = (*k8sRuntime)(nil)
