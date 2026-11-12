package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
)

func main() {
	mode := flag.String("mode", "list", "list | impersonate | exec | prepare")
	kubeconfig := flag.String("kubeconfig", "", "path to kubeconfig")
	namespace := flag.String("ns", "default", "namespace")
	pod := flag.String("pod", "", "pod name for exec")
	user := flag.String("user", "alice", "impersonated user")
	flag.Parse()

	if *kubeconfig == "" {
		fmt.Fprintln(os.Stderr, "-kubeconfig is required")
		os.Exit(2)
	}

	baseCfg, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	must(err)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	switch *mode {
	case "list":
		doList(ctx, baseCfg, *namespace)
	case "impersonate":
		doImpersonate(ctx, baseCfg, *namespace, *user)
	case "prepare":
		doPrepare(ctx, baseCfg, *namespace)
	case "exec":
		if *pod == "" {
			fmt.Fprintln(os.Stderr, "-pod is required for exec")
			os.Exit(2)
		}
		doExec(ctx, baseCfg, *namespace, *pod)
	default:
		fmt.Fprintln(os.Stderr, "unknown mode:", *mode)
		os.Exit(2)
	}
}

// doList 使用 dynamic client 列 Pod，验证：discovery + 非结构化资源访问。
func doList(ctx context.Context, cfg *rest.Config, ns string) {
	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	must(err)

	// 真实环境里的重要细节：某个聚合 API（如 metrics-server）不可用时，
	// ServerPreferredResources 会整体报错。必须容忍"部分发现失败"，
	// 否则一个不健康的聚合 API 会让整个资源浏览功能瘫痪。
	rl, discErr := dc.ServerPreferredResources()
	if discErr != nil {
		if !discovery.IsGroupDiscoveryFailedError(discErr) {
			must(discErr)
		}
		fmt.Printf("[list] WARN partial discovery failure (tolerated): %v\n", discErr)
	}

	hasPods := false
	for _, list := range rl {
		for _, r := range list.APIResources {
			if r.Name == "pods" && strings.Contains(list.GroupVersion, "v1") {
				hasPods = true
			}
		}
	}
	fmt.Printf("[list] discovery ok, preferred resource groups: %d, pods discovered: %v\n", len(rl), hasPods)

	dyn, err := dynamic.NewForConfig(cfg)
	must(err)
	gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	l, err := dyn.Resource(gvr).Namespace(ns).List(ctx, metav1.ListOptions{Limit: 20})
	must(err)
	fmt.Printf("[list] namespace=%s pod count=%d\n", ns, len(l.Items))
	for _, item := range l.Items {
		phase, _, _ := unstructured.NestedString(item.Object, "status", "phase")
		fmt.Printf("  - %-45s phase=%s\n", item.GetName(), phase)
	}
}

// doPrepare 创建一个可 exec 的测试 Pod（busybox），用于验证 exec 链路。
func doPrepare(ctx context.Context, cfg *rest.Config, ns string) {
	cs, err := kubernetes.NewForConfig(cfg)
	must(err)

	name := "kubedeck-exec-test"
	_, err = cs.CoreV1().Pods(ns).Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		fmt.Printf("[prepare] pod %s already exists\n", name)
		return
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"app": "kubedeck-exec-test"}},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{{
				Name:            "main",
				Image:           "busybox:1.36",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Command:         []string{"sh", "-c", "sleep 3600"},
			}},
		},
	}
	_, err = cs.CoreV1().Pods(ns).Create(ctx, pod, metav1.CreateOptions{})
	must(err)
	fmt.Printf("[prepare] created pod %s/%s, waiting for Ready...\n", ns, name)

	deadline := time.Now().Add(150 * time.Second)
	for time.Now().Before(deadline) {
		p, err := cs.CoreV1().Pods(ns).Get(ctx, name, metav1.GetOptions{})
		if err == nil {
			for _, cond := range p.Status.Conditions {
				if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
					fmt.Println("[prepare] pod is Ready")
					return
				}
			}
		}
		time.Sleep(3 * time.Second)
	}
	fmt.Println("[prepare] timed out waiting for Ready; check image pull / events")
}

// doImpersonate 验证 Impersonation + K8S 原生 RBAC。
func doImpersonate(ctx context.Context, cfg *rest.Config, ns, user string) {
	// 1) 以被模拟身份访问 default，预期 403（尚未授权）
	impCfg := rest.CopyConfig(cfg)
	impCfg.Impersonate = rest.ImpersonationConfig{UserName: user, Groups: []string{"kubedeck:developers"}}

	impClient, err := kubernetes.NewForConfig(impCfg)
	must(err)

	_, err = impClient.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{Limit: 1})
	fmt.Printf("[impersonate] step1 user=%s ns=%s before-grant: err=%v\n", user, ns, shortErr(err))
	if err == nil {
		fmt.Println("[impersonate] WARN: expected Forbidden before granting RBAC, got success")
	}

	// 2) 管理员创建 Role/RoleBinding，授予该用户当前 namespace 的 pod 只读权限
	admin, err := kubernetes.NewForConfig(cfg)
	must(err)

	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: "kubedeck-view-pods"},
		Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{""},
			Resources: []string{"pods"},
			Verbs:     []string{"get", "list", "watch"},
		}},
	}
	_, err = admin.RbacV1().Roles(ns).Create(ctx, role, metav1.CreateOptions{})
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		must(err)
	}

	binding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "kubedeck-view-pods"},
		Subjects:   []rbacv1.Subject{{Kind: "User", Name: user, APIGroup: rbacv1.GroupName}},
		RoleRef:    rbacv1.RoleRef{Kind: "Role", Name: "kubedeck-view-pods", APIGroup: rbacv1.GroupName},
	}
	_, err = admin.RbacV1().RoleBindings(ns).Create(ctx, binding, metav1.CreateOptions{})
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		must(err)
	}

	// 3) 再以被模拟身份访问，预期成功
	l, err := impClient.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{Limit: 5})
	fmt.Printf("[impersonate] step2 after-grant: err=%v", shortErr(err))
	if err == nil {
		fmt.Printf(" pods=%d", len(l.Items))
	}
	fmt.Println()

	// 4) 跨 namespace 访问，预期 403（证明 RBAC 边界生效）
	_, err = impClient.CoreV1().Pods("kube-system").List(ctx, metav1.ListOptions{Limit: 1})
	fmt.Printf("[impersonate] step3 cross-namespace kube-system: err=%v\n", shortErr(err))

	// 5) SelfSubjectAccessReview 预检
	ssar := &authorizationv1.SelfSubjectAccessReview{
		Spec: authorizationv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Namespace: ns,
				Verb:      "list",
				Resource:  "pods",
			},
		},
	}
	resp, err := impClient.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, ssar, metav1.CreateOptions{})
	if err == nil {
		fmt.Printf("[impersonate] SSAR allowed=%v reason=%q\n", resp.Status.Allowed, resp.Status.Reason)
	} else {
		fmt.Printf("[impersonate] SSAR err=%v\n", shortErr(err))
	}
}

// doExec 验证 SPdy/WebSocket exec 链路。
func doExec(ctx context.Context, cfg *rest.Config, ns, pod string) {
	req := kubernetes.NewForConfigOrDie(cfg).CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(ns).
		Name(pod).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Command: []string{"sh", "-c", "echo exec-ok; hostname; id"},
			Stdout:  true,
			Stderr:  true,
		}, scheme.ParameterCodec)

	fmt.Printf("[exec] target %s/%s\n", ns, pod)
	exec, err := remotecommand.NewSPDYExecutor(cfg, "POST", req.URL())
	if err != nil {
		fmt.Fprintf(os.Stderr, "[exec] NewSPDYExecutor error: %v\n", err)
		os.Exit(1)
	}
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{Stdout: os.Stdout, Stderr: os.Stderr})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[exec] stream error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("[exec] success")
}

func shortErr(err error) string {
	if err == nil {
		return "<nil>"
	}
	s := err.Error()
	if len(s) > 160 {
		s = s[:160] + "..."
	}
	return s
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
}