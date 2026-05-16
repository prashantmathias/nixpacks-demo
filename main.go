package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

func main() {
	namespace := "default"

	// 1. Initialize client-go
	var kubeconfig string
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = filepath.Join(home, ".kube", "config")
	} else {
		fmt.Println("Could not find kubeconfig")
		os.Exit(1)
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		panic(err.Error())
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	// Read target-app source files
	mainTsBytes, err := os.ReadFile("target-app/main.ts")
	if err != nil {
		panic(fmt.Errorf("failed to read target-app/main.ts: %w", err))
	}
	denoJsonBytes, err := os.ReadFile("target-app/deno.json")
	if err != nil {
		panic(fmt.Errorf("failed to read target-app/deno.json: %w", err))
	}

	runID := uuid.New().String()[:8]
	configMapName := fmt.Sprintf("buildkit-build-context-%s", runID)
	podName := fmt.Sprintf("buildkit-builder-%s", runID)
	imageName := fmt.Sprintf("ttl.sh/buildkit-proto-%s:1h", runID)
	deploymentName := fmt.Sprintf("buildkit-app-%s", runID)

	fmt.Printf("Starting BuildKit prototype run %s\n", runID)
	fmt.Printf("Image destination will be: %s\n", imageName)

	ctx := context.Background()

	// 2. Create ConfigMap
	fmt.Println("-> Creating ConfigMap with source files...")
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: configMapName,
		},
		Data: map[string]string{
			"main.ts":   string(mainTsBytes),
			"deno.json": string(denoJsonBytes),
		},
	}
	_, err = clientset.CoreV1().ConfigMaps(namespace).Create(ctx, cm, metav1.CreateOptions{})
	if err != nil {
		panic(err.Error())
	}
	defer func() {
		// Clean up configmap at end
		clientset.CoreV1().ConfigMaps(namespace).Delete(ctx, configMapName, metav1.DeleteOptions{})
	}()

	// Helper function for pointer to bool
	priv := true

	// 3. Launch BuildKit Pod
	fmt.Println("-> Launching BuildKit Pod...")
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: podName,
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:  "buildkitd",
					Image: "moby/buildkit:latest",
					SecurityContext: &corev1.SecurityContext{
						Privileged: &priv,
					},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "buildkit-socket", MountPath: "/run/buildkit"},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							// Guaranteed CPU/memory floor for the daemon
							corev1.ResourceCPU:    resource.MustParse("500m"),
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
				},
				{
					Name:       "builder",
					Image:      "ubuntu:latest",
					WorkingDir: "/workspace",
					Command:    []string{"sh", "-c"},
					Args: []string{
						"apt-get update && apt-get install -y curl && " +
							"curl -sSL https://nixpacks.com/install.sh | bash && " +
							"curl -sSL https://github.com/moby/buildkit/releases/download/v0.12.5/buildkit-v0.12.5.linux-amd64.tar.gz | tar xz -C /usr/local/bin --strip-components=1 && " +
							"cp -L -r /workspace-cm/* /workspace/ && " +
							"nixpacks build . -o . && " +
							"while ! buildctl --addr unix:///run/buildkit/buildkitd.sock debug workers; do sleep 1; done && " +
							"buildctl --addr unix:///run/buildkit/buildkitd.sock build --frontend dockerfile.v0 --local context=/workspace --local dockerfile=/workspace/.nixpacks --output type=image,name=" + imageName + ",push=true",
					},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "workspace-cm", MountPath: "/workspace-cm"},
						{Name: "workspace-volume", MountPath: "/workspace"},
						{Name: "buildkit-socket", MountPath: "/run/buildkit"},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							// Guaranteed CPU/memory floor for the control process
							corev1.ResourceCPU:    resource.MustParse("500m"),
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "workspace-cm",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: configMapName,
							},
						},
					},
				},
				{
					Name: "workspace-volume",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
				{
					Name: "buildkit-socket",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
			},
		},
	}
	_, err = clientset.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		panic(err.Error())
	}

	defer func() {
		// Cleanup buildkit pod at end
		clientset.CoreV1().Pods(namespace).Delete(ctx, podName, metav1.DeleteOptions{})
	}()

	// 4. Watch & Wait for Pod Completion
	fmt.Println("-> Waiting for BuildKit build to complete (this may take a minute)...")
	watch, err := clientset.CoreV1().Pods(namespace).Watch(ctx, metav1.ListOptions{
		FieldSelector: "metadata.name=" + podName,
	})
	if err != nil {
		panic(err.Error())
	}

	buildSuccess := false
	for event := range watch.ResultChan() {
		p, ok := event.Object.(*corev1.Pod)
		if !ok {
			continue
		}

		var builderStatus *corev1.ContainerStatus
		for _, cs := range p.Status.ContainerStatuses {
			if cs.Name == "builder" {
				builderStatus = &cs
				break
			}
		}

		if builderStatus != nil && builderStatus.State.Terminated != nil {
			if builderStatus.State.Terminated.ExitCode == 0 {
				buildSuccess = true
				fmt.Println("-> BuildKit build SUCCEEDED!")
				break
			} else {
				fmt.Println("-> BuildKit build FAILED!")
				cmd := exec.Command("kubectl", "logs", podName, "-c", "builder", "-n", namespace)
				out, _ := cmd.CombinedOutput()
				fmt.Printf("BuildKit Logs:\n%s\n", string(out))
				break
			}
		}

		if p.Status.Phase == corev1.PodFailed {
			fmt.Println("-> BuildKit pod FAILED entirely!")
			cmd := exec.Command("kubectl", "logs", podName, "--all-containers", "-n", namespace)
			out, _ := cmd.CombinedOutput()
			fmt.Printf("BuildKit Logs:\n%s\n", string(out))
			break
		}
	}
	watch.Stop()

	if !buildSuccess {
		fmt.Println("Exiting due to build failure. Run `kubectl logs " + podName + "` quickly to see why if it hasn't been cleaned up.")
		os.Exit(1)
	}

	// Wait briefly to ensure ttl.sh registers the image
	time.Sleep(5 * time.Second)

	// 5. Deploy the newly built image
	fmt.Println("-> Creating Deployment...")
	replicas := int32(1)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: deploymentName,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": deploymentName,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": deploymentName,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "web",
							Image: imageName,
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									Protocol:      corev1.ProtocolTCP,
									ContainerPort: 8080,
								},
							},
						},
					},
				},
			},
		},
	}
	_, err = clientset.AppsV1().Deployments(namespace).Create(ctx, deployment, metav1.CreateOptions{})
	if err != nil {
		panic(err.Error())
	}

	fmt.Println("-> Creating Service...")
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: deploymentName + "-svc",
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": deploymentName,
			},
			Ports: []corev1.ServicePort{
				{
					Protocol:   corev1.ProtocolTCP,
					Port:       80,
					TargetPort: intstr.FromInt(8080),
					NodePort:   30080,
				},
			},
			Type: corev1.ServiceTypeNodePort,
		},
	}

	_, err = clientset.CoreV1().Services(namespace).Create(ctx, service, metav1.CreateOptions{})
	if err != nil {
		if strings.Contains(err.Error(), "provided port is already allocated") {
			fmt.Println("-> Port 30080 is allocated, falling back to dynamic NodePort")
			service.Spec.Ports[0].NodePort = 0 // Let kubernetes assign
			_, err = clientset.CoreV1().Services(namespace).Create(ctx, service, metav1.CreateOptions{})
			if err != nil {
				panic(err.Error())
			}
		} else {
			panic(err.Error())
		}
	}

	finalSvc, err := clientset.CoreV1().Services(namespace).Get(ctx, deploymentName+"-svc", metav1.GetOptions{})
	var assignedPort int32
	if err == nil && len(finalSvc.Spec.Ports) > 0 {
		assignedPort = finalSvc.Spec.Ports[0].NodePort
	}

	fmt.Println("=====================================================")
	fmt.Println("Prototype Execution Complete!")
	fmt.Printf("Deployment created: %s\n", deploymentName)
	if assignedPort > 0 {
		fmt.Printf("You can access the app at: http://localhost:%d (assuming Docker Desktop/minikube port forwarding)\n", assignedPort)
	} else {
		fmt.Println("Service created.")
	}
	fmt.Println("To clean up:")
	fmt.Printf("kubectl delete deployment %s\n", deploymentName)
	fmt.Printf("kubectl delete service %s-svc\n", deploymentName)
	fmt.Println("=====================================================")
}
