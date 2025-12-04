/*
Copyright 2021 IBM All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"path"
	"strconv"
	"sync"
	"time"

	"github.com/hyperledger/fabric-gateway/pkg/client"
	"github.com/hyperledger/fabric-gateway/pkg/identity"
	"github.com/hyperledger/fabric-protos-go-apiv2/gateway"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

const (
	mspID        = "Org1MSP"
	cryptoPath   = "../crypto-config/peerOrganizations/org1.example.com"
	certPath     = cryptoPath + "/users/User1@org1.example.com/msp/signcerts"
	keyPath      = cryptoPath + "/users/User1@org1.example.com/msp/keystore"
	tlsCertPath  = cryptoPath + "/peers/peer0.org1.example.com/tls/ca.crt"
	peerEndpoint = "dns:///localhost:7051"
	gatewayPeer  = "peer0.org1.example.com"
	ordererCA    = "/opt/gopath/src/github.com/hyperledger/fabric/peer/crypto/ordererOrganizations/example.com/tlsca/tlsca.example.com-cert.pem"
)

// Estrutura para armazenar parâmetros
type BatchParameters struct {
	BatchTimeout string
	BatchSize    int
}

// AQUI: Definição do tipo no nível do pacote.
type txResult struct {
	Index        int
	Start        time.Time
	End          time.Time
	Latency      time.Duration
	Success      bool
	ErrorMessage string
}

var now = time.Now()

//var assetId = fmt.Sprintf("asset%d", now.Unix()*1e3+int64(now.Nanosecond())/1e6)

func main() {
	// The gRPC client connection should be shared by all Gateway connections to this endpoint
	clientConnection := newGrpcConnection()
	defer clientConnection.Close()

	id := newIdentity()
	sign := newSign()

	// Create a Gateway connection for a specific client identity
	gw, err := client.Connect(
		id,
		client.WithSign(sign),
		client.WithClientConnection(clientConnection),
		// Default timeouts for different gRPC calls
		client.WithEvaluateTimeout(5*time.Second),
		client.WithEndorseTimeout(15*time.Second),
		client.WithSubmitTimeout(5*time.Second),
		client.WithCommitStatusTimeout(1*time.Minute),
	)
	if err != nil {
		panic(err)
	}
	defer gw.Close()

	// Override default values for chaincode and channel name as they may differ in testing contexts.
	//chaincodeName := "fabcar"
	chaincodeName := "basic"
	if ccname := os.Getenv("CHAINCODE_NAME"); ccname != "" {
		chaincodeName = ccname
	}

	channelName := "mychannel"
	if cname := os.Getenv("CHANNEL_NAME"); cname != "" {
		channelName = cname
	}

	network := gw.GetNetwork(channelName)
	contract := network.GetContract(chaincodeName)

	// Switch baseado no argumento passado
	operacao := os.Args[1]
	switch operacao {
	case "initLedger":
		initLedger(contract)
	case "getAllAssets":
		getAllAssets(contract)
	case "createAsset":
		n := 1 // Valor padrão para criar um asset
		if len(os.Args) >= 3 {
			numAssets, err := strconv.Atoi(os.Args[2])
			if err == nil {
				n = numAssets
			} else {
				fmt.Println("Erro ao converter número de assets, usando o valor padrão de 1.")
			}
		}
		createAssets(contract, n)
	case "readAssetByID":
		if len(os.Args) < 3 {
			fmt.Println("Uso: go run main.go readAssetByID <assetId>")
			return
		}
		assetId := os.Args[2]
		readAssetByID(contract, assetId)
	case "transferAsset":
		if len(os.Args) < 4 {
			fmt.Println("Uso: go run main.go transferAssetAsync <assetId> <newOwner>")
			return
		}
		assetId := os.Args[2]
		newOwner := os.Args[3]
		transferAssetAsync(contract, assetId, newOwner)
	case "createAssetBench":
		tps := 10        // Valor padrão para TPS
		numAssets := 100 // Número padrão de assets a serem criados
		if len(os.Args) >= 3 {
			tpsVal, err := strconv.Atoi(os.Args[2])
			if err == nil {
				tps = tpsVal
			} else {
				fmt.Println("Error converting TPS, using default value of 10.")
			}
		}

		if len(os.Args) >= 4 {
			numAssetsVal, err := strconv.Atoi(os.Args[3])
			if err == nil {
				numAssets = numAssetsVal
			} else {
				fmt.Println("Error converting number of assets, using default value of 100.")
			}
		}
		createAssetBench(contract, tps, numAssets)
	case "createAssetBenchTime":
		tps := 10                    // Valor padrão para TPS
		duration := 10 * time.Second // Duração padrão para o benchmark (10 segundos)

		if len(os.Args) >= 3 {
			tpsVal, err := strconv.Atoi(os.Args[2])
			if err == nil {
				tps = tpsVal
			} else {
				fmt.Println("Error converting TPS, using default value of 10.")
			}
		}

		if len(os.Args) >= 4 {
			durationVal, err := strconv.Atoi(os.Args[3])
			if err == nil {
				duration = time.Duration(durationVal) * time.Second
			} else {
				fmt.Println("Error converting duration, using default value of 10 seconds.")
			}
		}

		createAssetBenchTime(contract, tps, duration) // Atualização para a nova função

	case "createAssetEndorse":
		var num int
		var err error
		if len(os.Args) == 3 {
			num, err = strconv.Atoi(os.Args[2])
			if err != nil {
				log.Fatalf("Número inválido: %v", err)
			}
		} else {
			num = 1 // Valor padrão
		}
		createAssetEndorse(contract, num)
	case "createAssetBenchDetailed":
		if len(os.Args) < 4 {
			log.Fatalf("Uso: %s createAssetBenchDetailed <TPS> <Número de Ativos>", os.Args[0])
		}
		tps, err := strconv.Atoi(os.Args[2])
		if err != nil {
			log.Fatalf("TPS inválido: %v", err)
		}
		numAssets, err := strconv.Atoi(os.Args[3])
		if err != nil {
			log.Fatalf("Número de Ativos inválido: %v", err)
		}
		createAssetBenchDetailed(contract, tps, numAssets)
	case "createAssetBenchEnd":
		if len(os.Args) < 4 {
			log.Fatalf("Uso: %s createAssetBench <TPS> <Número de Ativos>", os.Args[0])
		}
		tps, err := strconv.Atoi(os.Args[2])
		if err != nil {
			log.Fatalf("TPS inválido: %v", err)
		}
		numAssets, err := strconv.Atoi(os.Args[3])
		if err != nil {
			log.Fatalf("Número de Ativos inválido: %v", err)
		}
		createAssetBenchEnd(contract, tps, numAssets)
	case "exampleErrorHandling":
		exampleErrorHandling(contract)
	default:
		fmt.Println("Operation not recognized.")
	}
}

// newGrpcConnection creates a gRPC connection to the Gateway server.
func newGrpcConnection() *grpc.ClientConn {
	certificatePEM, err := os.ReadFile(tlsCertPath)
	if err != nil {
		panic(fmt.Errorf("failed to read TLS certifcate file: %w", err))
	}

	certificate, err := identity.CertificateFromPEM(certificatePEM)
	if err != nil {
		panic(err)
	}

	certPool := x509.NewCertPool()
	certPool.AddCert(certificate)
	transportCredentials := credentials.NewClientTLSFromCert(certPool, gatewayPeer)

	connection, err := grpc.NewClient(peerEndpoint, grpc.WithTransportCredentials(transportCredentials))
	if err != nil {
		panic(fmt.Errorf("failed to create gRPC connection: %w", err))
	}

	return connection
}

// newIdentity creates a client identity for this Gateway connection using an X.509 certificate.
func newIdentity() *identity.X509Identity {
	certificatePEM, err := readFirstFile(certPath)
	if err != nil {
		panic(fmt.Errorf("failed to read certificate file: %w", err))
	}

	certificate, err := identity.CertificateFromPEM(certificatePEM)
	if err != nil {
		panic(err)
	}

	id, err := identity.NewX509Identity(mspID, certificate)
	if err != nil {
		panic(err)
	}

	return id
}

// newSign creates a function that generates a digital signature from a message digest using a private key.
func newSign() identity.Sign {
	privateKeyPEM, err := readFirstFile(keyPath)
	if err != nil {
		panic(fmt.Errorf("failed to read private key file: %w", err))
	}

	privateKey, err := identity.PrivateKeyFromPEM(privateKeyPEM)
	if err != nil {
		panic(err)
	}

	sign, err := identity.NewPrivateKeySign(privateKey)
	if err != nil {
		panic(err)
	}

	return sign
}

func readFirstFile(dirPath string) ([]byte, error) {
	dir, err := os.Open(dirPath)
	if err != nil {
		return nil, err
	}

	fileNames, err := dir.Readdirnames(1)
	if err != nil {
		return nil, err
	}

	return os.ReadFile(path.Join(dirPath, fileNames[0]))
}

/*
var methods = []string{
	"InitLedger",
	"CreateCar",
	"QueryAllCars",
	"QueryCar",
	"ChangeCarOwner",
}
*/

var methods = []string{
	"InitLedger",
	"CreateAsset",
	"GetAllAssets",
	"ReadAsset",
	"TransferAsset",
}

func generateRandomHash() string {
	// Gerar uma string aleatória de 8 bytes
	randomBytes := make([]byte, 8)
	_, err := rand.Read(randomBytes)
	if err != nil {
		panic(fmt.Errorf("erro ao gerar bytes aleatórios: %v", err))
	}
	randomString := fmt.Sprintf("%x", randomBytes)

	// Calcular o hash SHA-256 da string aleatória
	hash := sha256.New()
	hash.Write([]byte(randomString))
	hashInBytes := hash.Sum(nil)

	// Converter o hash em uma string hexadecimal
	hashString := fmt.Sprintf("%x", hashInBytes)

	return hashString
}

// This type of transaction would typically only be run once by an application the first time it was started after its
// initial deployment. A new version of the chaincode deployed later would likely not need to run an "init" function.
func initLedger(contract *client.Contract) {
	fmt.Printf("\n--> Submit Transaction: InitLedger, function creates the initial set of assets on the ledger \n")

	_, err := contract.SubmitTransaction(methods[0])
	if err != nil {
		panic(fmt.Errorf("failed to submit transaction: %w", err))
	}

	fmt.Printf("*** Transaction committed successfully\n")
}

// Evaluate a transaction to query ledger state.
func getAllAssets(contract *client.Contract) {
	fmt.Println("\n--> Evaluate Transaction: GetAllAssets, function returns all the current assets on the ledger")

	evaluateResult, err := contract.EvaluateTransaction(methods[2])
	if err != nil {
		panic(fmt.Errorf("failed to evaluate transaction: %w", err))
	}
	result := formatJSON(evaluateResult)

	fmt.Printf("*** Result:%s\n", result)
}

// Submit transactions synchronously, blocking until each has been committed to the ledger.
func createAssets(contract *client.Contract, n int) {
	if n <= 0 {
		n = 1 // Set n to 1 if it's zero or negative
	}

	fmt.Printf("\n--> Submit Transactions: CreateAsset, creates %d new assets with ID, Color, Size, Owner, and AppraisedValue arguments\n", n)

	for i := 0; i < n; i++ {
		// Assuming generateRandomHash() is defined elsewhere to generate a random hash
		hash := generateRandomHash()

		startTime := time.Now()

		_, err := contract.SubmitTransaction(methods[1], hash, "yellow", "5", "Tom", "1300")
		if err != nil {
			panic(fmt.Errorf("failed to submit transaction: %w", err))
		}

		endTime := time.Now()
		elapsedTime := endTime.Sub(startTime)

		fmt.Printf("*** Transaction %s committed successfully\n", hash)
		fmt.Printf("Time taken: %v\n", elapsedTime)
	}
}

func createAssetBench(contract *client.Contract, tps int, numAssets int) {
	// 1. Validação de Entrada
	if tps <= 0 {
		fmt.Println("Invalid TPS value. Please provide a positive integer.")
		return
	}
	if numAssets <= 0 {
		numAssets = 1
	}
	// Supondo que 'methods' é uma variável global/do pacote
	// if len(methods) < 2 {
	//     fmt.Println("methods slice does not contain enough elements.")
	//     return
	// }

	fmt.Printf("\n--> Benchmarking CreateAsset: %d Transações a %d TPS\n", numAssets, tps)

	// Intervalo de tempo entre o INÍCIO de cada transação (1/TPS)
	interval := time.Second / time.Duration(tps)

	startTime := time.Now()
	var wg sync.WaitGroup
	wg.Add(numAssets)

	// Pré-alocação do slice de resultados.
	// Seguro porque cada goroutine escreverá em seu índice exclusivo.
	results := make([]txResult, numAssets)

	// 2. Loop de Envio (Controle de Taxa)
	for i := 0; i < numAssets; i++ {
		// Dispara a goroutine para a transação 'i'
		go func(i int) {
			defer wg.Done()

			// ❌ time.Sleep removido daqui. O controle de ritmo é externo.

			hash := generateRandomHash()

			txStartTime := time.Now()
			_, err := contract.SubmitTransaction(methods[1], hash, "yellow", "5", "Tom", "1300")
			txEndTime := time.Now()

			// Escrita no índice pré-alocado (i)
			if err != nil {
				results[i] = txResult{i, txStartTime, txEndTime, txEndTime.Sub(txStartTime), false, err.Error()}
				return
			}

			results[i] = txResult{i, txStartTime, txEndTime, txEndTime.Sub(txStartTime), true, ""}
		}(i)

		// 🟢 CORREÇÃO: Dormir APÓS INICIAR a goroutine para garantir o TPS.
		// O loop espera pelo 'interval' antes de iniciar a PRÓXIMA transação.
		time.Sleep(interval)
	}

	// 3. Aguardar Conclusão e Medir Tempo Total
	fmt.Println("\nWaiting for all started transactions to complete...")
	wg.Wait()
	endTime := time.Now()
	elapsedTime := endTime.Sub(startTime) // Tempo total de execução

	// 4. Tabela detalhada
	fmt.Printf("\n*** Transações Individuais ***\n")
	fmt.Println("---------------------------------------------------------------------------------------------------------------")
	fmt.Printf("| %-5s | %-30s | %-30s | %-10s | %-8s |\n", "ID", "Start", "End", "Latency(ms)", "Success")
	fmt.Println("---------------------------------------------------------------------------------------------------------------")

	var (
		latenciesMs            []float64
		successfulTransactions int
		totalLatency           float64
	)

	for _, res := range results {
		latencyMs := float64(res.Latency.Milliseconds())
		status := "NO"
		if res.Success {
			status = "YES"
			latenciesMs = append(latenciesMs, latencyMs)
			successfulTransactions++
			totalLatency += latencyMs
		}
		fmt.Printf("| %-5d | %-30s | %-30s | %-10.2f | %-8s |\n",
			res.Index,
			res.Start.Format("2006-01-02 15:04:05.000"),
			res.End.Format("2006-01-02 15:04:05.000"),
			latencyMs,
			status)
	}
	fmt.Println("---------------------------------------------------------------------------------------------------------------")

	// 5. Cálculo de métricas agregadas
	var stdDev float64
	var meanLatency float64
	if successfulTransactions > 0 {
		meanLatency = totalLatency / float64(successfulTransactions)
		for _, l := range latenciesMs {
			stdDev += math.Pow(l-meanLatency, 2)
		}
		// Evita divisão por zero
		if len(latenciesMs) > 0 {
			stdDev = math.Sqrt(stdDev / float64(len(latenciesMs)))
		} else {
			stdDev = 0
		}
	}

	tpsAchieved := float64(successfulTransactions) / elapsedTime.Seconds()

	// 6. Tabela resumida
	fmt.Printf("\n*** Benchmarking Summary ***\n")
	fmt.Printf("-------------------------------------------------------------------------------------------------------------------\n")
	fmt.Printf("| TPS Configurado | Transações Enviadas | Sucesso | Tempo Total | TPS Real | Latência Média (ms) | Desvio Padrão (ms) |\n")
	fmt.Printf("-------------------------------------------------------------------------------------------------------------------\n")
	fmt.Printf("| %-15d | %-20d | %-7d | %-11s | %-8.2f | %-20.2f | %-20.2f |\n",
		tps, numAssets, successfulTransactions, elapsedTime.Truncate(time.Millisecond), tpsAchieved, meanLatency, stdDev)
	fmt.Printf("-------------------------------------------------------------------------------------------------------------------\n")
}

// createAssetBenchTime executa um benchmark de criação de ativos
// controlando a taxa de envio de transações (TPS) e a duração total.
func createAssetBenchTime(contract *client.Contract, tps int, duration time.Duration) {
	// 1. Validação de Entrada
	if tps <= 0 {
		fmt.Println("Invalid TPS value. Please provide a positive integer.")
		return
	}
	if duration <= 0 {
		fmt.Println("Invalid duration value. Please provide a positive duration.")
		return
	}

	fmt.Printf("\n--> Benchmarking CreateAsset at %d TPS for %s\n", tps, duration.Truncate(time.Second))

	// Variáveis Compartilhadas e Proteção
	var (
		wg                sync.WaitGroup
		mu                sync.Mutex // Mutex para proteger o acesso a 'results'
		totalTransactions int        // Total de transações INICIADAS
		results           []txResult // Coleta dos resultados
	)

	// Configuração do Controlador de Taxa
	// O intervalo é o tempo entre o início de cada transação (1/TPS)
	interval := time.Second / time.Duration(tps)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Configuração do Controlador de Duração
	timeout := time.After(duration) // Canal que fecha após a duração do benchmark

	startTime := time.Now() // Início real do benchmark (início do loop de envio)

	// 2. Loop de Controle de Taxa e Duração
loop:
	for {
		select {
		case <-ticker.C:
			// Ticker disparou: iniciar uma nova transação.

			// Incrementa o contador de transações iniciadas antes de disparar a goroutine
			currentTxIndex := totalTransactions
			totalTransactions++

			wg.Add(1)
			go func(index int) {
				defer wg.Done()

				// Lógica de Submissão da Transação
				hash := generateRandomHash()
				txStartTime := time.Now()
				// Chamada de transação: Assumindo que methods[1] é a string "CreateAsset"
				_, err := contract.SubmitTransaction(methods[1], hash, "yellow", "5", "Tom", "1300")
				txEndTime := time.Now()

				// 🔴 Proteger o acesso ao slice 'results'
				mu.Lock()
				if err != nil {
					results = append(results, txResult{
						Index:        index,
						Start:        txStartTime,
						End:          txEndTime,
						Latency:      txEndTime.Sub(txStartTime),
						Success:      false,
						ErrorMessage: err.Error(),
					})
				} else {
					results = append(results, txResult{
						Index:   index,
						Start:   txStartTime,
						End:     txEndTime,
						Latency: txEndTime.Sub(txStartTime),
						Success: true,
					})
				}
				mu.Unlock() // Libera o Mutex
			}(currentTxIndex)

		case <-timeout:
			// O tempo 'duration' especificado terminou.
			break loop // Sai do loop principal de envio
		}
	}

	// 3. Aguardar Conclusão
	fmt.Println("\nWaiting for all started transactions to complete...")
	wg.Wait()
	endTime := time.Now()
	totalDuration := endTime.Sub(startTime) // Tempo total real gasto

	// 4. Output das Transações Individuais
	fmt.Printf("\n*** Transações Individuais ***\n")
	fmt.Println("---------------------------------------------------------------------------------------------------------------")
	fmt.Printf("| %-5s | %-30s | %-30s | %-10s | %-8s |\n", "ID", "Start", "End", "Latency(ms)", "Success")
	fmt.Println("---------------------------------------------------------------------------------------------------------------")

	var (
		latenciesMs            []float64
		successfulTransactions int
		totalLatency           float64
	)

	// Processar resultados
	for _, res := range results {
		latencyMs := float64(res.Latency.Milliseconds())
		status := "NO"
		if res.Success {
			status = "YES"
			latenciesMs = append(latenciesMs, latencyMs)
			successfulTransactions++
			totalLatency += latencyMs
		}
		fmt.Printf("| %-5d | %-30s | %-30s | %-10.2f | %-8s |\n",
			res.Index,
			res.Start.Format("2006-01-02 15:04:05.000"),
			res.End.Format("2006-01-02 15:04:05.000"),
			latencyMs,
			status)
	}
	fmt.Println("---------------------------------------------------------------------------------------------------------------")

	// 5. Calcular Métricas Agregadas
	var stdDev float64
	var meanLatency float64
	if successfulTransactions > 0 {
		meanLatency = totalLatency / float64(successfulTransactions)

		// Cálculo do Desvio Padrão
		for _, l := range latenciesMs {
			stdDev += math.Pow(l-meanLatency, 2)
		}
		// Evita divisão por zero
		if len(latenciesMs) > 0 {
			stdDev = math.Sqrt(stdDev / float64(len(latenciesMs)))
		} else {
			stdDev = 0
		}
	}

	tpsAchieved := float64(successfulTransactions) / totalDuration.Seconds()

	// 6. Display do Resumo
	fmt.Printf("\n*** Benchmarking Summary ***\n")
	fmt.Printf("-------------------------------------------------------------------------------------------------------------------\n")
	fmt.Printf("| TPS Configurado | Transações Enviadas | Sucesso | Tempo Total | TPS Real | Latência Média (ms) | Desvio Padrão (ms) |\n")
	fmt.Printf("-------------------------------------------------------------------------------------------------------------------\n")
	fmt.Printf("| %-15d | %-20d | %-7d | %-11s | %-8.2f | %-20.2f | %-20.2f |\n",
		tps, totalTransactions, successfulTransactions, totalDuration.Truncate(time.Millisecond), tpsAchieved, meanLatency, stdDev)
	fmt.Printf("-------------------------------------------------------------------------------------------------------------------\n")
}

func createAssetEndorse(contract *client.Contract, n int) {
	if n <= 0 {
		n = 1 // Set n to 1 if it's zero or negative
	}

	var totalEndorseTime, totalOrderingTime, totalCommitTime, totalElapsedTime time.Duration
	successfulTransactions := 0

	//fmt.Printf("\n--> Submit Transactions: CreateAsset, creates %d new assets with ID, Color, Size, Owner, and AppraisedValue arguments\n", n)

	for i := 0; i < n; i++ {
		hash := generateRandomHash()

		// Medir o tempo de endosso
		startTime := time.Now()
		proposal, err := contract.NewProposal(methods[1], client.WithArguments(hash, "yellow", "5", "Tom", "1300"))
		if err != nil {
			panic(fmt.Errorf("failed to create proposal: %w", err))
		}

		endorseStartTime := time.Now()
		transaction, err := proposal.Endorse()
		if err != nil {
			fmt.Printf("*** Endorsement failed for transaction %s\n", hash)
			continue
		}
		endorseEndTime := time.Now()
		endorseTime := endorseEndTime.Sub(endorseStartTime)
		totalEndorseTime += endorseTime

		// Medir o tempo de ordenação
		orderingStartTime := time.Now()
		commit, err := transaction.Submit()
		if err != nil {
			fmt.Printf("*** Ordering failed for transaction %s\n", hash)
			continue
		}
		orderingEndTime := time.Now()
		orderingTime := orderingEndTime.Sub(orderingStartTime)
		totalOrderingTime += orderingTime

		// Medir o tempo de commit
		commitStartTime := time.Now()
		status, err := commit.Status()
		if err != nil || !status.Successful {
			fmt.Printf("*** Commit failed for transaction %s\n", hash)
			continue
		}
		commitEndTime := time.Now()
		commitTime := commitEndTime.Sub(commitStartTime)
		totalCommitTime += commitTime

		// Tempo total da transação
		endTime := time.Now()
		elapsedTime := endTime.Sub(startTime)
		totalElapsedTime += elapsedTime

		fmt.Printf("*** Transaction %s committed successfully\n", hash)
		successfulTransactions++
	}

	// Cálculos finais
	averageEndorseTime := totalEndorseTime / time.Duration(successfulTransactions)
	averageOrderingTime := totalOrderingTime / time.Duration(successfulTransactions)
	averageCommitTime := totalCommitTime / time.Duration(successfulTransactions)
	averageTotalTime := totalElapsedTime / time.Duration(successfulTransactions)
	tps := float64(successfulTransactions) / totalElapsedTime.Seconds()

	// Exibir os resultados em uma tabela
	fmt.Printf("----------------------------------------------------------------------------------------------------------------------------\n")
	fmt.Printf("| %-23s | %-23s | %-12s | %-13s | %-11s | %-10s | %-12s |\n",
		"Transactions executed", "Successful Transactions", "Endorse Time", "Ordering Time", "Commit Time", "Total Time", "TPS achieved")
	fmt.Printf("| %-23d | %-23d | %-12v | %-13v | %-11v | %-10v | %-12.2f |\n",
		n, successfulTransactions, averageEndorseTime, averageOrderingTime, averageCommitTime, averageTotalTime, tps)
	fmt.Printf("----------------------------------------------------------------------------------------------------------------------------\n")

}

func createAssetBenchDetailed(contract *client.Contract, tps int, numAssets int) {
	if tps <= 0 {
		fmt.Println("Invalid TPS value. Please provide a positive integer.")
		return
	}
	if numAssets <= 0 {
		numAssets = 1
	}

	interval := time.Second / time.Duration(tps)

	var wg sync.WaitGroup
	wg.Add(numAssets)

	// Metrics collection
	var successfulTransactions int

	// Channels to collect latencies and other times
	latencyCh := make(chan time.Duration, numAssets)
	endorseTimeCh := make(chan time.Duration, numAssets)
	orderingTimeCh := make(chan time.Duration, numAssets)
	commitTimeCh := make(chan time.Duration, numAssets)

	// Print the header for the CSV output
	fmt.Println("Transaction,Endorse Time (ms),Ordering Time (ms),Commit Time (ms),Total Time (ms),Latency (ms),Timestamp (ms)")

	for i := 0; i < numAssets; i++ {
		go func(i int) {
			defer wg.Done()

			time.Sleep(time.Duration(i) * interval) // Distribute transactions over the interval

			hash := generateRandomHash()

			// Start of endorse time measurement
			endorseStartTime := time.Now()
			proposal, err := contract.NewProposal("CreateAsset", client.WithArguments(hash, "yellow", "5", "Tom", "1300"))
			if err != nil {
				fmt.Printf("failed to create proposal: %v\n", err)
				return
			}
			transaction, err := proposal.Endorse()
			if err != nil {
				fmt.Printf("failed to endorse transaction: %v\n", err)
				return
			}
			endorseEndTime := time.Now()
			endorseTime := endorseEndTime.Sub(endorseStartTime)
			endorseTimeCh <- endorseTime

			// Start of ordering time measurement
			orderingStartTime := time.Now()
			commit, err := transaction.Submit()
			if err != nil {
				fmt.Printf("failed to submit transaction: %v\n", err)
				return
			}
			orderingEndTime := time.Now()
			orderingTime := orderingEndTime.Sub(orderingStartTime)
			orderingTimeCh <- orderingTime

			// Start of commit time measurement
			commitStartTime := time.Now()
			status, err := commit.Status()
			if err != nil || !status.Successful {
				fmt.Printf("failed to commit transaction: %v\n", err)
				return
			}
			commitEndTime := time.Now()
			commitTime := commitEndTime.Sub(commitStartTime)
			commitTimeCh <- commitTime

			// Increment successful transactions count
			successfulTransactions++

			// Calculate total time and latency
			totalTime := endorseTime + orderingTime + commitTime
			latency := totalTime // A latência deve ser igual ao tempo total da transação

			// Send the latency to the channel
			latencyCh <- latency

			// Get timestamp in milliseconds

			// Print detailed transaction data in CSV format, including timestamp
			txEndTime := time.Now()
			fmt.Printf("%d,%.3f,%.3f,%.3f,%.3f,%.3f,%d\n",
				i+1,
				float64(endorseTime.Milliseconds()),
				float64(orderingTime.Milliseconds()),
				float64(commitTime.Milliseconds()),
				float64(totalTime.Milliseconds()),
				float64(latency.Milliseconds()),
				//txEndTime.UnixNano()/int64(time.Millisecond)
				txEndTime.UnixNano()/int64(time.Millisecond)) // Timestamp in ms
		}(i)
	}

	wg.Wait()

	// Close channels after waiting for goroutines to finish
	close(latencyCh)
	close(endorseTimeCh)
	close(orderingTimeCh)
	close(commitTimeCh)
}

func createAssetBenchEnd(contract *client.Contract, tps int, numAssets int) {
	if tps <= 0 {
		fmt.Println("Invalid TPS value. Please provide a positive integer.")
		return
	}
	if numAssets <= 0 {
		numAssets = 1
	}

	interval := time.Second / time.Duration(tps)

	var wg sync.WaitGroup
	wg.Add(numAssets)

	// Metrics collection
	var successfulTransactions int
	var mu sync.Mutex // To synchronize access to successfulTransactions

	// Channels to collect latencies and other times
	latencyCh := make(chan time.Duration, numAssets)
	endorseTimeCh := make(chan time.Duration, numAssets)
	orderingTimeCh := make(chan time.Duration, numAssets)
	commitTimeCh := make(chan time.Duration, numAssets)

	startTime := time.Now() // Start overall timer

	for i := 0; i < numAssets; i++ {
		go func(i int) {
			defer wg.Done()

			time.Sleep(time.Duration(i) * interval) // Distribute transactions over the interval

			hash := generateRandomHash()

			// Start of endorse time measurement
			endorseStartTime := time.Now()
			proposal, err := contract.NewProposal("CreateAsset", client.WithArguments(hash, "yellow", "5", "Tom", "1300"))
			if err != nil {
				fmt.Printf("Failed to create proposal: %v\n", err)
				return
			}
			transaction, err := proposal.Endorse()
			if err != nil {
				fmt.Printf("Failed to endorse transaction: %v\n", err)
				return
			}
			endorseEndTime := time.Now()
			endorseTime := endorseEndTime.Sub(endorseStartTime)
			endorseTimeCh <- endorseTime

			// Start of ordering time measurement
			orderingStartTime := time.Now()
			commit, err := transaction.Submit()
			if err != nil {
				fmt.Printf("Failed to submit transaction: %v\n", err)
				return
			}
			orderingEndTime := time.Now()
			orderingTime := orderingEndTime.Sub(orderingStartTime)
			orderingTimeCh <- orderingTime

			// Start of commit time measurement
			commitStartTime := time.Now()
			status, err := commit.Status()
			if err != nil || !status.Successful {
				fmt.Printf("Failed to commit transaction: %v\n", err)
				return
			}
			commitEndTime := time.Now()
			commitTime := commitEndTime.Sub(commitStartTime)
			commitTimeCh <- commitTime

			// Increment successful transactions count
			mu.Lock()
			successfulTransactions++
			mu.Unlock()

			// Calculate total time and latency
			totalTime := endorseTime + orderingTime + commitTime
			latencyCh <- totalTime
		}(i)
	}

	wg.Wait()

	// Close channels after waiting for goroutines to finish
	close(latencyCh)
	close(endorseTimeCh)
	close(orderingTimeCh)
	close(commitTimeCh)

	endTime := time.Now() // End overall timer
	elapsedTime := endTime.Sub(startTime)

	// Calculate average latencies and times
	var (
		totalEndorseTime  time.Duration
		totalOrderingTime time.Duration
		totalCommitTime   time.Duration
		totalLatency      time.Duration
	)

	// Collect results from channels
	for latency := range latencyCh {
		totalLatency += latency
	}
	for endorseTime := range endorseTimeCh {
		totalEndorseTime += endorseTime
	}
	for orderingTime := range orderingTimeCh {
		totalOrderingTime += orderingTime
	}
	for commitTime := range commitTimeCh {
		totalCommitTime += commitTime
	}

	if successfulTransactions == 0 {
		fmt.Println("No successful transactions. Cannot calculate metrics.")
		return
	}

	averageLatency := totalLatency / time.Duration(successfulTransactions)
	averageEndorseTime := totalEndorseTime / time.Duration(successfulTransactions)
	averageOrderingTime := totalOrderingTime / time.Duration(successfulTransactions)
	averageCommitTime := totalCommitTime / time.Duration(successfulTransactions)

	// Calculate TPS (Transactions Per Second)
	transactionsPerSecond := float64(successfulTransactions) / elapsedTime.Seconds()

	// Print results summary
	fmt.Printf("\n*** Benchmarking Complete ***\n")
	fmt.Printf("-------------------------------------------------------------------------------------------------------\n")
	fmt.Printf("| Transactions executed | Successful Transactions | Elapsed time   | TPS achieved | Average Latency   |\n")
	fmt.Printf("-------------------------------------------------------------------------------------------------------\n")
	fmt.Printf("| %-21d | %-23d | %-14s | %-12.2f | %-17s |\n",
		numAssets, successfulTransactions, elapsedTime.String(), transactionsPerSecond, averageLatency.String())
	fmt.Printf("-------------------------------------------------------------------------------------------------------\n")

	// Include detailed timing breakdown
	fmt.Printf("\nDetailed Timing Breakdown:\n")
	fmt.Printf("  Average Endorse Time: %s\n", averageEndorseTime)
	fmt.Printf("  Average Ordering Time: %s\n", averageOrderingTime)
	fmt.Printf("  Average Commit Time: %s\n", averageCommitTime)
	fmt.Printf("  Total Time Per Transaction: %s\n", averageLatency)
	// Após calcular métricas como totalOrderingTime, totalCommitTime e elapsedTime
	if successfulTransactions == 0 {
		fmt.Println("No successful transactions. Cannot calculate metrics.")
		return
	}

}

// Evaluate a transaction by assetID to query ledger state.
func readAssetByID(contract *client.Contract, assetId string) {
	fmt.Printf("\n--> Evaluate Transaction: ReadAsset, function returns asset attributes for asset ID: %s\n", assetId)

	evaluateResult, err := contract.EvaluateTransaction(methods[3], assetId)
	if err != nil {
		panic(fmt.Errorf("failed to evaluate transaction: %w", err))
	}
	result := formatJSON(evaluateResult)

	fmt.Printf("*** Result:%s\n", result)
}

// Submit transaction asynchronously, blocking until the transaction has been sent to the orderer, and allowing
// this thread to process the chaincode response (e.g. update a UI) without waiting for the commit notification
func transferAssetAsync(contract *client.Contract, assetId, newOwner string) {
	fmt.Printf("\n--> Async Submit Transaction: TransferAsset, updates existing asset owner")

	submitResult, commit, err := contract.SubmitAsync(methods[4], client.WithArguments(assetId, newOwner))
	if err != nil {
		panic(fmt.Errorf("failed to submit transaction asynchronously: %w", err))
	}

	fmt.Printf("\n*** Successfully submitted transaction to transfer ownership from %s to Mark. \n", string(submitResult))
	fmt.Println("*** Waiting for transaction commit.")

	if commitStatus, err := commit.Status(); err != nil {
		panic(fmt.Errorf("failed to get commit status: %w", err))
	} else if !commitStatus.Successful {
		panic(fmt.Errorf("transaction %s failed to commit with status: %d", commitStatus.TransactionID, int32(commitStatus.Code)))
	}

	fmt.Printf("*** Transaction committed successfully\n")
}

// Submit transaction, passing in the wrong number of arguments ,expected to throw an error containing details of any error responses from the smart contract.
func exampleErrorHandling(contract *client.Contract) {
	fmt.Println("\n--> Submit Transaction: UpdateAsset asset70, asset70 does not exist and should return an error")

	_, err := contract.SubmitTransaction("UpdateAsset", "asset70", "blue", "5", "Tomoko", "300")
	if err == nil {
		panic("******** FAILED to return an error")
	}

	fmt.Println("*** Successfully caught the error:")

	var endorseErr *client.EndorseError
	var submitErr *client.SubmitError
	var commitStatusErr *client.CommitStatusError
	var commitErr *client.CommitError

	if errors.As(err, &endorseErr) {
		fmt.Printf("Endorse error for transaction %s with gRPC status %v: %s\n", endorseErr.TransactionID, status.Code(endorseErr), endorseErr)
	} else if errors.As(err, &submitErr) {
		fmt.Printf("Submit error for transaction %s with gRPC status %v: %s\n", submitErr.TransactionID, status.Code(submitErr), submitErr)
	} else if errors.As(err, &commitStatusErr) {
		if errors.Is(err, context.DeadlineExceeded) {
			fmt.Printf("Timeout waiting for transaction %s commit status: %s", commitStatusErr.TransactionID, commitStatusErr)
		} else {
			fmt.Printf("Error obtaining commit status for transaction %s with gRPC status %v: %s\n", commitStatusErr.TransactionID, status.Code(commitStatusErr), commitStatusErr)
		}
	} else if errors.As(err, &commitErr) {
		fmt.Printf("Transaction %s failed to commit with status %d: %s\n", commitErr.TransactionID, int32(commitErr.Code), err)
	} else {
		panic(fmt.Errorf("unexpected error type %T: %w", err, err))
	}

	// Any error that originates from a peer or orderer node external to the gateway will have its details
	// embedded within the gRPC status error. The following code shows how to extract that.
	statusErr := status.Convert(err)

	details := statusErr.Details()
	if len(details) > 0 {
		fmt.Println("Error Details:")

		for _, detail := range details {
			switch detail := detail.(type) {
			case *gateway.ErrorDetail:
				fmt.Printf("- address: %s, mspId: %s, message: %s\n", detail.Address, detail.MspId, detail.Message)
			}
		}
	}
}

// Format JSON data
func formatJSON(data []byte) string {
	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, data, "", "  "); err != nil {
		panic(fmt.Errorf("failed to parse JSON: %w", err))
	}
	return prettyJSON.String()
}
