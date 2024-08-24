package main

import (
	"context"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"

	"github.com/tealeg/xlsx"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	metricsv "k8s.io/metrics/pkg/client/clientset/versioned"
)

// NodeMetrics enthält Metriken für einen einzelnen Knoten.
//
// Diese Struktur wird verwendet, um verschiedene Metriken für einen bestimmten Knoten im Kubernetes-Cluster zu speichern.
// Sie enthält Informationen über die physische CPU und den physischen Speicher des Knotens sowie die angeforderten, begrenzten und genutzten Ressourcen.
type NodeMetrics struct {
	Name            string            // Der Name des Knotens
	NodeType        string            // Der Typ des Knotens (z.B. "master", "worker")
	PhysicalCPU     string            // Die physische CPU des Knotens
	PhysicalMemory  string            // Der physische Speicher des Knotens
	RequestedCPU    string            // Die angeforderte CPU des Knotens
	RequestedMemory string            // Der angeforderte Speicher des Knotens
	LimitsCPU       string            // Die begrenzte CPU des Knotens
	LimitsMemory    string            // Der begrenzte Speicher des Knotens
	UsedCPU         string            // Die genutzte CPU des Knotens
	UsedMemory      string            // Der genutzte Speicher des Knotens
	Labels          map[string]string // Neues Feld für Labels
}

// ClusterMetrics enthält aggregierte Metriken für den gesamten Cluster.
//
// Diese Struktur wird verwendet, um aggregierte Metriken für alle Knoten im Kubernetes-Cluster zu speichern.
// Sie enthält eine Liste von NodeMetrics-Strukturen sowie die Gesamtsummen der physischen, angeforderten, begrenzten und genutzten Ressourcen.
type ClusterMetrics struct {
	Nodes                []NodeMetrics // Eine Liste von NodeMetrics-Strukturen für alle Knoten im Cluster
	TotalPhysicalCPU     string        // Die Gesamtsumme der physischen CPU im Cluster
	TotalPhysicalMemory  string        // Die Gesamtsumme des physischen Speichers im Cluster
	TotalRequestedCPU    string        // Die Gesamtsumme der angeforderten CPU im Cluster
	TotalRequestedMemory string        // Die Gesamtsumme des angeforderten Speichers im Cluster
	TotalLimitsCPU       string        // Die Gesamtsumme der begrenzten CPU im Cluster
	TotalLimitsMemory    string        // Die Gesamtsumme des begrenzten Speichers im Cluster
	TotalUsedCPU         string        // Die Gesamtsumme der genutzten CPU im Cluster
	TotalUsedMemory      string        // Die Gesamtsumme des genutzten Speichers im Cluster
}

var nodeType, serviceaccountname, kubeconfig *string // Globale Variablen für den Knotentyp und den Service-Account
var serverMode *bool                                 // Globale Variable für den Servermodus
// Initialize the cluster metrics struct
var clusterMetrics ClusterMetrics

// main ist der Einstiegspunkt der Anwendung.
//
// Diese Funktion parst die Befehlszeilen-Flags und entscheidet, ob die Anwendung im Servermodus oder im CLI-Modus ausgeführt wird.
// Im Servermodus wird ein HTTP-Server gestartet, der Metriken für Knoten im Kubernetes-Cluster sammelt und anzeigt.
// Im CLI-Modus werden die Metriken direkt in der Konsole angezeigt.
func main() {
	// Parse command-line flags
	nodeType, serverMode, serviceaccountname, kubeconfig = getFlags()
	log.Println("\nKnotentyp: ", *nodeType, "\nServermodus: ", *serverMode, "\nService-Account: ", *serviceaccountname, "\nKubeconfig: ", *kubeconfig)

	// Check if running in server mode
	if *serverMode {
		// Ausführung im Servermodus
		var clientset *kubernetes.Clientset
		var metricsClient *metricsv.Clientset

		// Check if running in a Kubernetes Pod
		if _, err := os.Stat("/var/run/secrets/kubernetes.io/serviceaccount/token"); err == nil {
			// Running in a Pod, use in-cluster configuration
			clientset, metricsClient = getPodClients()
		} else {
			// Not running in a Pod, use kubeconfig
			if *kubeconfig == "" {
				*kubeconfig = os.Getenv("KUBECONFIG")
				if *kubeconfig == "" {
					log.Fatalf("kubeconfig not provided and KUBECONFIG environment variable is not set")
				}
			}
			clientset, metricsClient = getClients(kubeconfig)
		}

		// Get the list of nodes in the cluster
		nodes := getNodes(clientset)
		fmt.Fprintln(os.Stderr, "Servermodus -- Knotentyp: ", *nodeType, "sammelt Metriken für Knoten")

		// HTTP-Handler für das /metrics-Endpunkt einrichten
		http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
			nodeType := r.URL.Query().Get("node-type")
			if nodeType == "" {
				nodeType = "all"
			}
			clusterMetrics = calculateClusterMetrics(clientset, metricsClient, nodes, nodeType)
			sortNodeMetricsByName(clusterMetrics.Nodes)
			renderTemplate(w, clusterMetrics)
			printASCIITable(clusterMetrics)
		})

		// HTTP-Handler für das /download/excel-Endpunkt einrichten
		http.HandleFunc("/download/excel", downloadExcelHandler)

		// Starten des HTTP-Servers
		log.Println("Server startet auf :8080")
		log.Fatal(http.ListenAndServe(":8080", nil))
	} else {
		// Ausführung im CLI-Modus
		clientset, metricsClient := getClients(kubeconfig)
		nodes := getNodes(clientset)

		fmt.Fprintln(os.Stderr, "CLI-Modus -- Knotentyp: ", *nodeType, "sammelt Metriken für Knoten")
		clusterMetrics = calculateClusterMetrics(clientset, metricsClient, nodes, *nodeType)
		sortNodeMetricsByName(clusterMetrics.Nodes)
		printASCIITable(clusterMetrics)
	}
}

// getFlags parst Befehlszeilen-Flags und gibt deren Werte zurück.
//
// Diese Funktion definiert und parst die Befehlszeilen-Flags, die für die Anwendung verwendet werden.
// Sie gibt die Werte der Flags als Zeiger zurück.
//
// Rückgabewerte:
// - *string: Ein Zeiger auf den Wert des "node-type"-Flags, der den Knotentyp angibt (z.B. "worker" oder "infra").
// - *bool: Ein Zeiger auf den Wert des "server"-Flags, der angibt, ob der Webserver gestartet werden soll.
// - *string: Ein Zeiger auf den Wert des "service-account"-Flags, der den Namen des Service-Accounts angibt.
//
// Beispiel:
//
//	nodeType, serverMode, sa := getFlags()
func getFlags() (*string, *bool, *string, *string) {
	// Definiere Befehlszeilen-Flags
	nodeType := flag.String("node-type", "worker", "Specify the node type (worker or infra)")
	serverMode := flag.Bool("server", false, "Start the web server")
	serviceaccountname := flag.String("sa", "scp", "Specify the service account name")
	kubeconfig := flag.String("kubeconfig", "", "Path to the kubeconfig file")

	// Parse die Befehlszeilen-Flags
	flag.Parse()

	// If kubeconfig is not set via CLI, use the KUBECONFIG environment variable
	if *kubeconfig == "" {
		*kubeconfig = os.Getenv("KUBECONFIG")
	}

	// Gib die Werte der Flags zurück
	return nodeType, serverMode, serviceaccountname, kubeconfig
}

// getPodClients erstellt Kubernetes- und Metrik-Clients unter Verwendung der In-Cluster-Konfiguration.
//
// Diese Funktion wird verwendet, um Kubernetes- und Metrik-Clients zu erstellen, die innerhalb eines Kubernetes-Clusters ausgeführt werden.
// Sie verwendet die In-Cluster-Konfiguration, um die notwendigen Verbindungsinformationen zu erhalten.
//
// Rückgabewerte:
// - *kubernetes.Clientset: Ein Clientset, das verwendet wird, um mit der Kubernetes-API zu kommunizieren.
// - *metricsv.Clientset: Ein Clientset, das verwendet wird, um Metriken von Kubernetes-Ressourcen abzurufen.
//
// Fehler:
// Diese Funktion beendet das Programm mit einem log.Fatalf-Aufruf, wenn ein Fehler auftritt, z.B. beim Erstellen der In-Cluster-Konfiguration,
// beim Abrufen des aktuellen Namespaces oder beim Abrufen des Tokens aus dem Secret.
//
// Beispiel:
//
//	clientset, metricsClient := getPodClients()
//
// Ablauf:
// 1. Erstellt die In-Cluster-Konfiguration.
// 2. Erstellt ein neues Kubernetes-Clientset.
// 3. Ruft den aktuellen Namespace ab.
// 4. Ruft das Token aus dem Secret ab, das mit dem Service-Account verknüpft ist.
// 5. Setzt das BearerToken in der Konfiguration auf das abgerufene Token.
// 6. Erstellt ein neues Metrik-Clientset unter Verwendung der aktualisierten Konfiguration.
// 7. Gibt das Kubernetes-Clientset und das Metrik-Clientset zurück.
func getPodClients() (*kubernetes.Clientset, *metricsv.Clientset) {
	// Create in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("Error creating in-cluster config: %v", err)
	} else {
		log.Println("InClusterConfig", config)
	}

	// Create a new Kubernetes clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Error creating Kubernetes client: %v", err)
	} else {
		log.Println("KubernetesClient", clientset)
	}

	// Get the current namespace
	namespace, err := getCurrentNamespace()
	if err != nil {
		log.Fatalf("Error getting current namespace: %v", err)
	} else {
		log.Println("Namespace", namespace)
	}

	// Get the token from the secret associated with the service account
	token, err := getTokenFromSecret(clientset, namespace, *serviceaccountname)
	if err != nil {
		log.Fatalf("Error getting token from secret: %v", err)
	} else {
		log.Println("Token", token)
	}

	// Set the BearerToken in the config to the token retrieved from the secret
	config.BearerToken = token

	// Create a new metrics client using the updated config
	metricsClient, err := metricsv.NewForConfig(config)
	if err != nil {
		log.Fatalf("Error creating metrics client: %v", err)
	}

	// Return the Kubernetes clientset and metrics client
	return clientset, metricsClient
}

// getClients erstellt Kubernetes- und Metrik-Clients unter Verwendung der bereitgestellten kubeconfig-Datei.
//
// Diese Funktion wird verwendet, um Kubernetes- und Metrik-Clients zu erstellen, die außerhalb eines Kubernetes-Clusters ausgeführt werden.
// Sie verwendet die bereitgestellte kubeconfig-Datei, um die notwendigen Verbindungsinformationen zu erhalten.
//
// Parameter:
// - kubeconfig: Ein Zeiger auf einen String, der den Pfad zur kubeconfig-Datei enthält.
//
// Rückgabewerte:
// - *kubernetes.Clientset: Ein Clientset, das verwendet wird, um mit der Kubernetes-API zu kommunizieren.
// - *metricsv.Clientset: Ein Clientset, das verwendet wird, um Metriken von Kubernetes-Ressourcen abzurufen.
//
// Fehler:
// Diese Funktion beendet das Programm mit einem log.Fatalf-Aufruf, wenn ein Fehler auftritt, z.B. beim Erstellen der Konfiguration
// aus der kubeconfig-Datei oder beim Erstellen der Kubernetes- oder Metrik-Clients.
//
// Beispiel:
//
//	kubeconfig := "/path/to/kubeconfig"
//	clientset, metricsClient := getClients(&kubeconfig)
//
// Ablauf:
// 1. Erstellt die Konfiguration aus der kubeconfig-Datei.
// 2. Erstellt ein neues Kubernetes-Clientset.
// 3. Erstellt ein neues Metrik-Clientset.
// 4. Gibt das Kubernetes-Clientset und das Metrik-Clientset zurück.
func getClients(kubeconfig *string) (*kubernetes.Clientset, *metricsv.Clientset) {
	// Build the config from the kubeconfig file
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		log.Fatalf("Error building kubeconfig: %v", err)
	}

	// Create a new Kubernetes clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Error creating Kubernetes client: %v", err)
	}

	// Create a new metrics client
	metricsClient, err := metricsv.NewForConfig(config)
	if err != nil {
		log.Fatalf("Error creating metrics client: %v", err)
	}

	// Return the Kubernetes clientset and metrics client
	return clientset, metricsClient
}

// getCurrentNamespace liest den aktuellen Namespace aus der Service-Account-Token-Datei.
//
// Diese Funktion wird verwendet, um den Namespace zu ermitteln, in dem der aktuelle Pod ausgeführt wird.
// Sie liest den Namespace aus der Datei "/var/run/secrets/kubernetes.io/serviceaccount/namespace", die vom Kubernetes-System bereitgestellt wird.
//
// Rückgabewerte:
// - string: Der aktuelle Namespace als String.
// - error: Ein Fehlerobjekt, falls ein Fehler beim Lesen der Datei auftritt.
//
// Fehler:
// Diese Funktion gibt einen Fehler zurück, wenn die Datei nicht gelesen werden kann.
//
// Beispiel:
//
//	namespace, err := getCurrentNamespace()
//	if err != nil {
//	    log.Fatalf("Error getting current namespace: %v", err)
//	}
//
// Ablauf:
// 1. Liest den Inhalt der Datei "/var/run/secrets/kubernetes.io/serviceaccount/namespace".
// 2. Gibt den Inhalt der Datei als String zurück.
// 3. Gibt einen Fehler zurück, falls das Lesen der Datei fehlschlägt.
func getCurrentNamespace() (string, error) {
	// Read the namespace from the file
	namespaceBytes, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return "", fmt.Errorf("error reading namespace file: %v", err)
	}
	return string(namespaceBytes), nil
}

// getTokenFromSecret ruft das Token aus dem Secret ab, das mit dem angegebenen Service-Account verknüpft ist.
//
// Diese Funktion wird verwendet, um das Token aus dem Secret eines bestimmten Service-Accounts in einem bestimmten Namespace abzurufen.
// Das Token wird benötigt, um authentifizierte Anfragen an die Kubernetes-API zu stellen.
//
// Parameter:
// - clientset: Ein Kubernetes-Clientset, das verwendet wird, um mit der Kubernetes-API zu kommunizieren.
// - namespace: Der Namespace, in dem sich der Service-Account befindet.
// - serviceAccountName: Der Name des Service-Accounts, dessen Token abgerufen werden soll.
//
// Rückgabewerte:
// - string: Das abgerufene Token als String.
// - error: Ein Fehlerobjekt, falls ein Fehler beim Abrufen des Service-Accounts oder des Secrets auftritt oder das Token nicht im Secret gefunden wird.
//
// Fehler:
// Diese Funktion gibt einen Fehler zurück, wenn:
// - Der Service-Account nicht abgerufen werden kann.
// - Der Service-Account keine Secrets hat.
// - Das Secret nicht abgerufen werden kann.
// - Das Token nicht im Secret gefunden wird.
//
// Beispiel:
//
//	token, err := getTokenFromSecret(clientset, "default", "my-service-account")
//	if err != nil {
//	    log.Fatalf("Error getting token from secret: %v", err)
//	}
//
// Ablauf:
// 1. Ruft den Service-Account im angegebenen Namespace ab.
// 2. Überprüft, ob der Service-Account Secrets hat.
// 3. Ruft das erste Secret des Service-Accounts ab.
// 4. Ruft das Token aus dem Secret ab.
// 5. Gibt das Token als String zurück.
func getTokenFromSecret(clientset *kubernetes.Clientset, namespace, serviceAccountName string) (string, error) {
	// Get the service account
	sa, err := clientset.CoreV1().ServiceAccounts(namespace).Get(context.TODO(), serviceAccountName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("error getting service account: %v", err)
	}

	// Check if the service account has any secrets
	if len(sa.Secrets) == 0 {
		return "", fmt.Errorf("no secrets found for service account: %s", serviceAccountName)
	}

	// Get the secret associated with the service account
	var secretName string
	for _, secret := range sa.Secrets {
		if strings.Contains(secret.Name, "token") {
			secretName = secret.Name
			break
		}
	}

	if secretName == "" {
		return "", fmt.Errorf("no secret with 'token' in the name found")
	}
	secret, err := clientset.CoreV1().Secrets(namespace).Get(context.TODO(), secretName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("error getting secret: %v", err)
	}

	// Retrieve the token from the secret
	token, ok := secret.Data["token"]
	if !ok {
		return "", fmt.Errorf("token not found in secret: %s", secretName)
	}

	return string(token), nil
}

// getNodes ruft die Liste der Knoten im Cluster ab.
//
// Diese Funktion wird verwendet, um eine Liste aller Knoten im Kubernetes-Cluster abzurufen.
// Sie verwendet das übergebene Kubernetes-Clientset, um die Knotenliste von der Kubernetes-API zu erhalten.
//
// Parameter:
// - clientset: Ein Kubernetes-Clientset, das verwendet wird, um mit der Kubernetes-API zu kommunizieren.
//
// Rückgabewerte:
// - *v1.NodeList: Eine Liste der Knoten im Cluster.
//
// Fehler:
// Diese Funktion beendet das Programm mit einem log.Fatalf-Aufruf, wenn ein Fehler beim Abrufen der Knotenliste auftritt.
//
// Beispiel:
//
//	nodes := getNodes(clientset)
//
// Ablauf:
// 1. Listet die Knoten im Cluster unter Verwendung des Kubernetes-Clientsets.
// 2. Gibt die Liste der Knoten zurück.
func getNodes(clientset *kubernetes.Clientset) *v1.NodeList {
	// List the nodes in the cluster
	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		log.Fatalf("Error listing nodes: %v", err)
	}
	return nodes
}

// calculateClusterMetrics berechnet die Metriken für den gesamten Cluster basierend auf den Metriken der einzelnen Knoten.
//
// Diese Funktion wird verwendet, um die Gesamtsummen der verschiedenen Metriken für den gesamten Cluster zu berechnen.
// Sie iteriert über alle Knoten im Cluster, berechnet die Metriken für jeden Knoten und summiert diese zu den Gesamtsummen.
//
// Parameter:
// - clientset: Ein Kubernetes-Clientset, das verwendet wird, um mit der Kubernetes-API zu kommunizieren.
// - metricsClient: Ein Clientset, das verwendet wird, um Metriken von Kubernetes-Ressourcen abzurufen.
// - nodes: Eine Liste der Knoten im Cluster.
// - nodeType: Der Typ der Knoten, für die die Metriken berechnet werden sollen (z.B. "master", "worker").
//
// Rückgabewerte:
// - ClusterMetrics: Eine Struktur, die die berechneten Metriken für den gesamten Cluster enthält.
//
// Beispiel:
//
//	clusterMetrics := calculateClusterMetrics(clientset, metricsClient, nodes, "worker")
//
// Ablauf:
// 1. Initialisiert Variablen für die Gesamtsummen der verschiedenen Metriken.
// 2. Verwendet eine WaitGroup, um die parallele Verarbeitung der Knoten zu synchronisieren.
// 3. Iteriert über alle Knoten im Cluster und startet eine Goroutine zur Berechnung der Metriken für jeden Knoten des angegebenen Typs.
// 4. Wartet, bis alle Goroutines abgeschlossen sind, und sammelt die Metriken der einzelnen Knoten.
// 5. Addiert die Metriken der einzelnen Knoten zu den Gesamtsummen.
// 6. Erstellt und gibt die ClusterMetrics-Struktur zurück.
func calculateClusterMetrics(clientset *kubernetes.Clientset, metricsClient *metricsv.Clientset, nodes *v1.NodeList, nodeType string) ClusterMetrics {
	// Initialisiere Variablen für die Gesamtsummen der verschiedenen Metriken
	var totalPhysicalCPU, totalPhysicalMemory, totalRequestedCPU, totalRequestedMem, totalLimitsCPU, totalLimitsMem, totalUsedCPU, totalUsedMem resource.Quantity
	var nodeMetricsList []NodeMetrics

	// Verwende einen WaitGroup, um die parallele Verarbeitung der Knoten zu synchronisieren
	var wg sync.WaitGroup
	nodeMetricsChan := make(chan NodeMetrics, len(nodes.Items))

	// Iteriere über alle Knoten im Cluster
	for _, node := range nodes.Items {
		// Überprüfe, ob der Knoten den angegebenen Node-Typ hat
		if _, isNodeType := node.Labels[fmt.Sprintf("node-role.kubernetes.io/%s", nodeType)]; isNodeType || nodeType == "all" {
			wg.Add(1)
			// Starte eine Goroutine zur Berechnung der Metriken für den Knoten
			go func(node v1.Node) {
				defer wg.Done()
				nodeMetrics := calculateNodeMetrics(clientset, metricsClient, node, nodeType)
				nodeMetricsChan <- nodeMetrics
			}(node)
		}
	}

	// Warte, bis alle Goroutines abgeschlossen sind
	wg.Wait()
	close(nodeMetricsChan)

	// Sammle die Metriken der einzelnen Knoten und addiere sie zu den Gesamtsummen
	for nodeMetrics := range nodeMetricsChan {
		nodeMetricsList = append(nodeMetricsList, nodeMetrics)
		totalPhysicalCPU.Add(resource.MustParse(nodeMetrics.PhysicalCPU))
		totalPhysicalMemory.Add(resource.MustParse(nodeMetrics.PhysicalMemory))
		totalRequestedCPU.Add(resource.MustParse(nodeMetrics.RequestedCPU))
		totalRequestedMem.Add(resource.MustParse(nodeMetrics.RequestedMemory))
		totalLimitsCPU.Add(resource.MustParse(nodeMetrics.LimitsCPU))
		totalLimitsMem.Add(resource.MustParse(nodeMetrics.LimitsMemory))
		totalUsedCPU.Add(resource.MustParse(nodeMetrics.UsedCPU))
		totalUsedMem.Add(resource.MustParse(nodeMetrics.UsedMemory))
	}

	// Erstelle und gib die ClusterMetrics-Struktur zurück
	return ClusterMetrics{
		Nodes:                nodeMetricsList,
		TotalPhysicalCPU:     convertCpuStr(totalPhysicalCPU),
		TotalPhysicalMemory:  convertMemStr(totalPhysicalMemory),
		TotalRequestedCPU:    convertCpuStr(totalRequestedCPU),
		TotalRequestedMemory: convertMemStr(totalRequestedMem),
		TotalLimitsCPU:       convertCpuStr(totalLimitsCPU),
		TotalLimitsMemory:    convertMemStr(totalLimitsMem),
		TotalUsedCPU:         convertCpuStr(totalUsedCPU),
		TotalUsedMemory:      convertMemStr(totalUsedMem),
	}
}

// calculateNodeMetrics berechnet die Metriken für einen einzelnen Knoten.
//
// Diese Funktion wird verwendet, um verschiedene Metriken für einen bestimmten Knoten im Kubernetes-Cluster zu berechnen.
// Sie sammelt die angeforderten und begrenzten Ressourcen aller Pods auf dem Knoten sowie die aktuellen Nutzungsmetriken des Knotens.
//
// Parameter:
// - clientset: Ein Kubernetes-Clientset, das verwendet wird, um mit der Kubernetes-API zu kommunizieren.
// - metricsClient: Ein Clientset, das verwendet wird, um Metriken von Kubernetes-Ressourcen abzurufen.
// - node: Der Knoten, für den die Metriken berechnet werden sollen.
// - nodeType: Der Typ des Knotens (z.B. "master", "worker").
//
// Rückgabewerte:
// - NodeMetrics: Eine Struktur, die die berechneten Metriken für den Knoten enthält.
//
// Fehler:
// Diese Funktion beendet das Programm mit einem log.Fatalf-Aufruf, wenn ein Fehler beim Abrufen der Pods oder der Metriken auftritt.
//
// Beispiel:
//
//	nodeMetrics := calculateNodeMetrics(clientset, metricsClient, node, "worker")
//
// Ablauf:
// 1. Initialisiert Variablen für die verschiedenen Metriken.
// 2. Listet alle Pods auf dem angegebenen Knoten auf.
// 3. Iteriert über alle Pods und deren Container, um die angeforderten und begrenzten Ressourcen zu summieren.
// 4. Holt die aktuellen Nutzungsmetriken für den Knoten.
// 5. Addiert die aktuellen Nutzungsmetriken zu den Gesamtsummen.
// 6. Holt die physische Kapazität des Knotens.
// 7. Erstellt und gibt die NodeMetrics-Struktur zurück.
func calculateNodeMetrics(clientset *kubernetes.Clientset, metricsClient *metricsv.Clientset, node v1.Node, nodeType string) NodeMetrics {
	// Initialisiere Variablen für die verschiedenen Metriken
	var nodeRequestedCPU, nodeRequestedMem, nodeLimitsCPU, nodeLimitsMem, nodeUsedCPU, nodeUsedMem resource.Quantity

	// Erfassen der Labels des Nodes
	labels := node.Labels

	// Bestimmen des tatsächlichen Node-Typs basierend auf den Labels
	actualNodeType := "unknown"
	if val, ok := labels["node-role.kubernetes.io/worker"]; ok && val == "" {
		actualNodeType = "worker"
	} else if val, ok := labels["node-role.kubernetes.io/master"]; ok && val == "" {
		actualNodeType = "master"
	} else if val, ok := labels["node-role.kubernetes.io/infra"]; ok && val == "" {
		actualNodeType = "infra"
	}

	// Liste alle Pods auf dem angegebenen Knoten auf
	pods, err := clientset.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{
		FieldSelector: fmt.Sprintf("spec.nodeName=%s", node.Name),
	})
	if err != nil {
		log.Fatalf("Error listing pods on node %s: %v", node.Name, err)
	}

	// Iteriere über alle Pods auf dem Knoten
	for _, pod := range pods.Items {
		// Iteriere über alle Container in jedem Pod
		for _, container := range pod.Spec.Containers {
			requests := container.Resources.Requests
			limits := container.Resources.Limits

			// Addiere die angeforderten Ressourcen des Containers zu den Gesamtsummen
			nodeRequestedCPU.Add(requests[v1.ResourceCPU])
			nodeRequestedMem.Add(requests[v1.ResourceMemory])
			nodeLimitsCPU.Add(limits[v1.ResourceCPU])
			nodeLimitsMem.Add(limits[v1.ResourceMemory])
		}
	}

	// Hole die aktuellen Nutzungsmetriken für den Knoten
	nodeMetrics, err := metricsClient.MetricsV1beta1().NodeMetricses().Get(context.TODO(), node.Name, metav1.GetOptions{})
	if err != nil {
		log.Fatalf("Error getting metrics for node %s: %v", node.Name, err)
	}

	// Addiere die aktuellen Nutzungsmetriken zu den Gesamtsummen
	nodeUsedCPU.Add(*nodeMetrics.Usage.Cpu())
	nodeUsedMem.Add(*nodeMetrics.Usage.Memory())

	// Hole die physische Kapazität des Knotens
	physicalCPU := node.Status.Capacity[v1.ResourceCPU]
	physicalMemory := node.Status.Capacity[v1.ResourceMemory]

	// Erstelle und gib die NodeMetrics-Struktur zurück
	return NodeMetrics{
		Name:            node.Name,
		NodeType:        actualNodeType,
		PhysicalCPU:     physicalCPU.String(),
		PhysicalMemory:  convertMemStr(physicalMemory),
		RequestedCPU:    convertCpuStr(nodeRequestedCPU),
		RequestedMemory: convertMemStr(nodeRequestedMem),
		LimitsCPU:       convertCpuStr(nodeLimitsCPU),
		LimitsMemory:    convertMemStr(nodeLimitsMem),
		UsedCPU:         convertCpuStr(nodeUsedCPU),
		UsedMemory:      convertMemStr(nodeUsedMem),
		Labels:          labels, // Labels hinzufügen
	}
}

// parseQuantity parst einen Ressourcen-String in eine resource.Quantity-Struktur.
//
// Diese Funktion wird verwendet, um einen Ressourcen-String (z.B. "500m", "1Gi") in eine resource.Quantity-Struktur zu parsen.
// Wenn ein Fehler beim Parsen auftritt, beendet die Funktion das Programm mit einem log.Fatalf-Aufruf.
//
// Parameter:
// - quantityStr: Ein String, der die Ressource darstellt.
//
// Rückgabewerte:
// - resource.Quantity: Die geparste resource.Quantity-Struktur.
//
// Beispiel:
//
//	quantity := parseQuantity("500m")
func parseQuantity(quantityStr string) resource.Quantity {
	quantity, err := resource.ParseQuantity(quantityStr)
	if err != nil {
		log.Fatalf("Error parsing quantity: %v", err)
	}
	return quantity
}

// convertCpuStr konvertiert eine resource.Quantity in einen String, der die CPU in Kernen darstellt.
//
// Diese Funktion wird verwendet, um eine resource.Quantity, die eine CPU-Ressource darstellt, in einen String zu konvertieren,
// der die CPU in Kernen darstellt. Die resultierende String-Darstellung hat zwei Dezimalstellen.
//
// Parameter:
// - quantity: Eine resource.Quantity, die die CPU-Ressource darstellt.
//
// Rückgabewerte:
// - string: Die CPU-Ressource als String in Kernen.
//
// Beispiel:
//
//	cpuStr := convertCpuStr(quantity)
func convertCpuStr(quantity resource.Quantity) string {
	return fmt.Sprintf("%.2f", float64(convertToMilli(&quantity).Value())/1000.0)
}

// convertMemStr konvertiert eine resource.Quantity in einen String, der den Speicher in GiB darstellt.
//
// Diese Funktion wird verwendet, um eine resource.Quantity, die eine Speicherressource darstellt, in einen String zu konvertieren,
// der den Speicher in GiB (Gibibyte) darstellt.
//
// Parameter:
// - quantity: Eine resource.Quantity, die die Speicherressource darstellt.
//
// Rückgabewerte:
// - string: Die Speicherressource als String in GiB.
//
// Beispiel:
//
//	memStr := convertMemStr(quantity)
func convertMemStr(quantity resource.Quantity) string {
	return fmt.Sprintf("%dGi", convertToGiga(&quantity).Value())
}

// convertToMilli konvertiert eine resource.Quantity in Milli-Einheiten.
//
// Diese Funktion wird verwendet, um eine resource.Quantity in Milli-Einheiten zu konvertieren.
// Die resultierende resource.Quantity hat den Wert in Milli-Einheiten.
//
// Parameter:
// - quantity: Ein Zeiger auf eine resource.Quantity, die konvertiert werden soll.
//
// Rückgabewerte:
// - *resource.Quantity: Eine neue resource.Quantity in Milli-Einheiten.
//
// Beispiel:
//
//	milliQuantity := convertToMilli(&quantity)
func convertToMilli(quantity *resource.Quantity) *resource.Quantity {
	value := quantity.ScaledValue(resource.Milli)
	return resource.NewQuantity(value, resource.BinarySI)
}

// convertToGiga konvertiert eine resource.Quantity in Giga-Einheiten.
//
// Diese Funktion wird verwendet, um eine resource.Quantity in Giga-Einheiten zu konvertieren.
// Die resultierende resource.Quantity hat den Wert in Giga-Einheiten.
//
// Parameter:
// - quantity: Ein Zeiger auf eine resource.Quantity, die konvertiert werden soll.
//
// Rückgabewerte:
// - *resource.Quantity: Eine neue resource.Quantity in Giga-Einheiten.
//
// Beispiel:
//
//	gigaQuantity := convertToGiga(&quantity)
func convertToGiga(quantity *resource.Quantity) *resource.Quantity {
	value := quantity.ScaledValue(resource.Giga)
	return resource.NewQuantity(value, resource.BinarySI)
}

// sortNodeMetricsByName sortiert eine Liste von NodeMetrics nach dem Namen der Knoten.
//
// Diese Funktion wird verwendet, um eine Liste von NodeMetrics-Strukturen alphabetisch nach dem Namen der Knoten zu sortieren.
//
// Parameter:
// - nodes: Eine Liste von NodeMetrics-Strukturen, die sortiert werden sollen.
//
// Beispiel:
//
//	sortNodeMetricsByName(nodeMetrics)
func sortNodeMetricsByName(nodes []NodeMetrics) {
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].Name < nodes[j].Name
	})
}

// renderTemplate rendert die HTML-Vorlage mit den übergebenen Cluster-Metriken und schreibt sie in den HTTP-Response-Writer.
//
// Diese Funktion wird verwendet, um eine HTML-Vorlage mit den übergebenen Cluster-Metriken zu rendern und das Ergebnis in den HTTP-Response-Writer zu schreiben.
// Die HTML-Vorlage zeigt eine Tabelle mit den Metriken der einzelnen Knoten sowie die Gesamtsummen der Metriken.
//
// Parameter:
// - w: Der HTTP-Response-Writer, in den die gerenderte HTML-Vorlage geschrieben wird.
// - clusterMetrics: Eine Struktur, die die Cluster-Metriken enthält, die in der HTML-Vorlage angezeigt werden sollen.
//
// Beispiel:
//
//	renderTemplate(responseWriter, clusterMetrics)
//
// Ablauf:
// 1. Definiert und parst die HTML-Vorlage.
// 2. Führt die Vorlage mit den übergebenen Cluster-Metriken aus und schreibt das Ergebnis in den HTTP-Response-Writer.
// 3. Loggt einen Fehler, falls das Ausführen der Vorlage fehlschlägt.
func renderTemplate(w http.ResponseWriter, clusterMetrics ClusterMetrics) {
	// Definiere und parse die HTML-Vorlage
	tmpl := template.Must(template.New("clusterMetrics").Parse(`
        <!DOCTYPE html>
        <html>
        <head>
            <title>Cluster Metrics</title>
            <style>
                .header-row, .total-row {
                    background-color: lightgray;
                    font-weight: bold;
                }
                .physical-metrics {
                    background-color: lightblue;
                }
                .requested-metrics {
                    background-color: #bddabd;
                }
                .limited-metrics {
                    background-color: #d4bbbb;
                }
                .used-metrics {
                    background-color: #dfb684;
                    color: darkblue;
                }
				.center-text {
					text-align: center;
				}
            </style>
        </head>
        <body>
            <h1>Cluster Metrics</h1>
            <table border="1">
                <tr class="header-row">
                    <th>Node</th>
                    <th>Node Type</th>
                    <th>Physical CPU (core)</th>
                    <th>Requested CPU (core)</th>
                    <th>Limits CPU (core)</th>
                    <th>Used CPU (core)</th>
                    <th>Physical Memory (Gi)</th>
                    <th>Requested Memory (Gi)</th>
                    <th>Limits Memory (Gi)</th>
                    <th>Used Memory (Gi)</th>
                </tr>
                {{ range .Nodes }}
                <tr title="{{ range $key, $value := .Labels }}{{ $key }}: {{ $value }}&#10;{{ end }}">
                    <td class="header-row">{{ .Name }}</td>
                    <td>{{ .NodeType }}</td>
                    <td class="physical-metrics center-text">{{ .PhysicalCPU }}</td>
                    <td class="requested-metrics center-text">{{ .RequestedCPU }}</td>
                    <td class="limited-metrics center-text">{{ .LimitsCPU }}</td>
                    <td class="used-metrics center-text">{{ .UsedCPU }}</td>
                    <td class="physical-metrics center-text">{{ .PhysicalMemory }}</td>
                    <td class="requested-metrics center-text">{{ .RequestedMemory }}</td>
                    <td class="limited-metrics center-text">{{ .LimitsMemory }}</td>
                    <td class="used-metrics center-text">{{ .UsedMemory }}</td>
                </tr>
                {{ end }}
                <tr class="total-row">
                    <th>Total</th>
                    <th></th>
                    <th class="physical-metrics">{{ .TotalPhysicalCPU }}</th>
                    <th class="requested-metrics">{{ .TotalRequestedCPU }}</th>
                    <th class="limited-metrics">{{ .TotalLimitsCPU }}</th>
                    <th class="used-metrics">{{ .TotalUsedCPU }}</th>
                    <th class="physical-metrics">{{ .TotalPhysicalMemory }}</th>
                    <th class="requested-metrics">{{ .TotalRequestedMemory }}</th>
                    <th class="limited-metrics">{{ .TotalLimitsMemory }}</th>
                    <th class="used-metrics">{{ .TotalUsedMemory }}</th>
                </tr>
            </table>
            <p>optional params worker: /metrics/?node-type=worker; infra: /metrics?node-type=infra; master: /metrics?node-type=master; all: /metrics</p>
            <p><a href="/download/excel">Download Excel</a></p>
        </body>
        </html>
    `))

	// Führe die Vorlage mit den übergebenen Cluster-Metriken aus und schreibe das Ergebnis in den HTTP-Response-Writer
	err := tmpl.Execute(w, clusterMetrics)
	if err != nil {
		// Logge einen Fehler, falls das Ausführen der Vorlage fehlschlägt
		log.Fatalf("Error executing template: %v", err)
	}
}

// printASCIITable druckt die Cluster-Metriken in einer ASCII-Tabelle auf die Standardausgabe.
//
// Diese Funktion wird verwendet, um die Cluster-Metriken in einer formatierten ASCII-Tabelle auf die Standardausgabe zu drucken.
// Die Tabelle zeigt die Metriken der einzelnen Knoten sowie die Gesamtsummen der Metriken.
//
// Parameter:
// - clusterMetrics: Eine Struktur, die die Cluster-Metriken enthält, die in der ASCII-Tabelle angezeigt werden sollen.
//
// Beispiel:
//
//	printASCIITable(clusterMetrics)
//
// Ablauf:
// 1. Erstellt einen neuen Tabwriter, um die Tabelle zu formatieren.
// 2. Druckt die Kopfzeile der Tabelle.
// 3. Iteriert über alle Knoten und druckt deren Metriken.
// 4. Druckt die Gesamtsummen der Metriken.
// 5. Flusht den Tabwriter, um sicherzustellen, dass alle Daten geschrieben werden.
func printASCIITable(clusterMetrics ClusterMetrics) {
	// Erstelle einen neuen Tabwriter, um die Tabelle zu formatieren
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', tabwriter.Debug)

	// Drucke die Kopfzeile der Tabelle
	fmt.Fprintln(w, "Node\t Node Type\t Physical CPU\t Requested CPU\t Limits CPU\t Used CPU\t Physical Memory (Gi)\t Requested Memory (Gi)\t Limits Memory (Gi)\t Used Memory (Gi)\t")

	// Iteriere über alle Knoten und drucke deren Metriken
	for _, node := range clusterMetrics.Nodes {
		fmt.Fprintf(w, "%s\t %s\t %s\t %s\t %s\t %s\t %s\t %s\t %s\t %s\t\n",
			node.Name, node.NodeType, node.PhysicalCPU, node.RequestedCPU, node.LimitsCPU, node.UsedCPU, node.PhysicalMemory, node.RequestedMemory, node.LimitsMemory, node.UsedMemory)
	}

	// Drucke die Gesamtsummen der Metriken
	fmt.Fprintf(w, "Total\t\t %s\t %s\t %s\t %s\t %s\t %s\t %s\t %s\t\n",
		clusterMetrics.TotalPhysicalCPU, clusterMetrics.TotalRequestedCPU, clusterMetrics.TotalLimitsCPU, clusterMetrics.TotalUsedCPU,
		clusterMetrics.TotalPhysicalMemory, clusterMetrics.TotalRequestedMemory, clusterMetrics.TotalLimitsMemory, clusterMetrics.TotalUsedMemory)

	// Flushe den Tabwriter, um sicherzustellen, dass alle Daten geschrieben werden
	w.Flush()
}

// generateExcelFile erstellt eine Excel-Datei mit den Cluster-Metriken und speichert sie auf dem Server.
//
// Diese Funktion wird verwendet, um die Cluster-Metriken in eine Excel-Datei zu konvertieren und die Datei auf dem Server zu speichern.
// Die Excel-Datei enthält eine Tabelle mit den Metriken der einzelnen Knoten sowie die Gesamtsummen der Metriken.
//
// Parameter:
// - filePath: Der Pfad, unter dem die Excel-Datei gespeichert werden soll.
// - clusterMetrics: Eine Struktur, die die Cluster-Metriken enthält, die in der Excel-Datei angezeigt werden sollen.
//
// Beispiel:
//
//	err := generateExcelFile("/path/to/file.xlsx", clusterMetrics)
//	if err != nil {
//		log.Fatalf("Error generating Excel file: %v", err)
//	}
//
// Ablauf:
// 1. Erstellt eine neue Excel-Datei.
// 2. Fügt ein neues Arbeitsblatt hinzu.
// 3. Fügt die Kopfzeile der Tabelle hinzu.
// 4. Fügt die Metriken der einzelnen Knoten zur Tabelle hinzu.
// 5. Fügt die Gesamtsummen der Metriken zur Tabelle hinzu.
// 6. Speichert die Excel-Datei auf dem Server.
func generateExcelFile(filePath string, clusterMetrics ClusterMetrics) error {
	// Erstelle eine neue Excel-Datei
	file := xlsx.NewFile()
	sheet, err := file.AddSheet("Cluster Metrics")
	if err != nil {
		return err
	}

	// Füge die Kopfzeile der Tabelle hinzu
	headerRow := sheet.AddRow()
	headers := []string{"Node", "Node Type", "Physical CPU (core)", "Requested CPU (core)", "Limits CPU (core)", "Used CPU (core)", "Physical Memory (Gi)", "Requested Memory (Gi)", "Limits Memory (Gi)", "Used Memory (Gi)"}
	for _, header := range headers {
		cell := headerRow.AddCell()
		cell.Value = header
	}

	// Füge die Metriken der einzelnen Knoten zur Tabelle hinzu
	for _, node := range clusterMetrics.Nodes {
		row := sheet.AddRow()
		row.AddCell().Value = node.Name
		row.AddCell().Value = node.NodeType
		PhysicalCPU, err := strconv.ParseFloat(strings.Replace(node.PhysicalCPU, "Gi", "", -1), 64)
		if err != nil {
			log.Fatalf("Error converting PhysicalCPU to float64: %v", err)
		}
		RequestedCPU, err := strconv.ParseFloat(strings.Replace(node.RequestedCPU, "Gi", "", -1), 64)
		if err != nil {
			log.Fatalf("Error converting RequestedCPU to float64: %v", err)
		}
		LimitsCPU, err := strconv.ParseFloat(strings.Replace(node.LimitsCPU, "Gi", "", -1), 64)
		if err != nil {
			log.Fatalf("Error converting LimitsCPU to float64: %v", err)
		}
		UsedCPU, err := strconv.ParseFloat(strings.Replace(node.UsedCPU, "Gi", "", -1), 64)
		if err != nil {
			log.Fatalf("Error converting UsedCPU to float64: %v", err)
		}
		PhysicalMemory, err := strconv.ParseFloat(strings.Replace(node.PhysicalMemory, "Gi", "", -1), 64)
		if err != nil {
			log.Fatalf("Error converting PhysicalMemory to float64: %v", err)
		}
		RequestedMemory, err := strconv.ParseFloat(strings.Replace(node.RequestedMemory, "Gi", "", -1), 64)
		if err != nil {
			log.Fatalf("Error converting RequestedMemory to float64: %v", err)
		}
		LimitsMemory, err := strconv.ParseFloat(strings.Replace(node.LimitsMemory, "Gi", "", -1), 64)
		if err != nil {
			log.Fatalf("Error converting LimitsMemory to float64: %v", err)
		}
		UsedMemory, err := strconv.ParseFloat(strings.Replace(node.UsedMemory, "Gi", "", -1), 64)
		if err != nil {
			log.Fatalf("Error converting UsedMemory to float64: %v", err)
		}
		row.AddCell().SetFloat(PhysicalCPU)
		row.AddCell().SetFloat(RequestedCPU)
		row.AddCell().SetFloat(LimitsCPU)
		row.AddCell().SetFloat(UsedCPU)
		row.AddCell().SetFloat(PhysicalMemory)
		row.AddCell().SetFloat(RequestedMemory)
		row.AddCell().SetFloat(LimitsMemory)
		row.AddCell().SetFloat(UsedMemory)
	}

	// Füge die Gesamtsummen der Metriken zur Tabelle hinzu
	totalRow := sheet.AddRow()
	totalRow.AddCell().Value = "Total"
	totalRow.AddCell().Value = ""
	TotalPhysicalCPU, err := strconv.ParseFloat(strings.Replace(clusterMetrics.TotalPhysicalCPU, "Gi", "", -1), 64)
	if err != nil {
		log.Fatalf("Error converting TotalPhysicalCPU to float64: %v", err)
	}
	TotalRequestedCPU, err := strconv.ParseFloat(strings.Replace(clusterMetrics.TotalRequestedCPU, "Gi", "", -1), 64)
	if err != nil {
		log.Fatalf("Error converting TotalRequestedCPU to float64: %v", err)
	}
	TotalLimitsCPU, err := strconv.ParseFloat(strings.Replace(clusterMetrics.TotalLimitsCPU, "Gi", "", -1), 64)
	if err != nil {
		log.Fatalf("Error converting TotalLimitsCPU to float64: %v", err)
	}
	TotalUsedCPU, err := strconv.ParseFloat(strings.Replace(clusterMetrics.TotalUsedCPU, "Gi", "", -1), 64)
	if err != nil {
		log.Fatalf("Error converting TotalUsedCPU to float64: %v", err)
	}
	TotalPhysicalMemory, err := strconv.ParseFloat(strings.Replace(clusterMetrics.TotalPhysicalMemory, "Gi", "", -1), 64)
	if err != nil {
		log.Fatalf("Error converting TotalPhysicalMemory to float64: %v", err)
	}
	TotalRequestedMemory, err := strconv.ParseFloat(strings.Replace(clusterMetrics.TotalRequestedMemory, "Gi", "", -1), 64)
	if err != nil {
		log.Fatalf("Error converting TotalRequestedMemory to float64: %v", err)
	}
	TotalLimitsMemory, err := strconv.ParseFloat(strings.Replace(clusterMetrics.TotalLimitsMemory, "Gi", "", -1), 64)
	if err != nil {
		log.Fatalf("Error converting TotalLimitsMemory to float64: %v", err)
	}
	TotalUsedMemory, err := strconv.ParseFloat(strings.Replace(clusterMetrics.TotalUsedMemory, "Gi", "", -1), 64)
	if err != nil {
		log.Fatalf("Error converting TotalUsedMemory to float64: %v", err)
	}
	totalRow.AddCell().SetFloat(TotalPhysicalCPU)
	totalRow.AddCell().SetFloat(TotalRequestedCPU)
	totalRow.AddCell().SetFloat(TotalLimitsCPU)
	totalRow.AddCell().SetFloat(TotalUsedCPU)
	totalRow.AddCell().SetFloat(TotalPhysicalMemory)
	totalRow.AddCell().SetFloat(TotalRequestedMemory)
	totalRow.AddCell().SetFloat(TotalLimitsMemory)
	totalRow.AddCell().SetFloat(TotalUsedMemory)

	// Speichere die Excel-Datei auf dem Server
	err = file.Save(filePath)
	if err != nil {
		return err
	}

	return nil
}

// downloadExcelHandler ist ein HTTP-Handler, der die Excel-Datei zum Download bereitstellt.
//
// Dieser Handler wird verwendet, um die Excel-Datei mit den Cluster-Metriken zum Download bereitzustellen.
// Die Excel-Datei wird auf dem Server gespeichert und kann über diesen Handler heruntergeladen werden.
//
// Beispiel:
//
//	http.HandleFunc("/download/excel", downloadExcelHandler)
//
// Ablauf:
// 1. Setzt den Content-Type und die Content-Disposition-Header, um den Download der Excel-Datei zu initiieren.
// 2. Öffnet die Excel-Datei und kopiert ihren Inhalt in den HTTP-Response-Writer.
// 3. Loggt einen Fehler, falls das Öffnen oder Kopieren der Datei fehlschlägt.
func downloadExcelHandler(w http.ResponseWriter, r *http.Request) {
	filePath := "/tmp/file.xlsx" // Pfad zur gespeicherten Excel-Datei

	// Setze den Content-Type und die Content-Disposition-Header
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", "attachment; filename=cluster_metrics.xlsx")

	// Beispielaufruf der generateExcelFile-Funktion
	err := generateExcelFile(filePath, clusterMetrics)
	if err != nil {
		log.Fatalf("Error generating Excel file: %v", err)
	}
	// Öffne die Excel-Datei
	file, err := os.Open(filePath)
	if err != nil {
		http.Error(w, "Unable to open Excel file", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	// Kopiere den Inhalt der Excel-Datei in den HTTP-Response-Writer
	_, err = io.Copy(w, file)
	if err != nil {
		http.Error(w, "Unable to copy Excel file", http.StatusInternalServerError)
		return
	}
}
