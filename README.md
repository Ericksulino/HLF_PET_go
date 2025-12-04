# HLF-PET

Este projeto, chamado HLF-PET, demonstra como interagir com uma rede Hyperledger Fabric usando um cliente escrito em Go. O código inclui operações básicas como inicializar o ledger, criar ativos, transferir ativos e consultar o ledger.

## Pré-requisitos

1. Go 1.21+ instalado.
2. Hyperledger Fabric test network configurada e em execução.
3. Certificados e chaves configurados no diretório correto.

## Estrutura do Projeto

```plaintext
.
├── client.go
├── go.mod
├── go.sum
└── README.md
```
## Instalação

1. Clone este repositório:

        git clone https://github.com/Ericksulino/HLF_PET_go.git
        cd HLF_PET_go

2. Baixe as dependências do Go:

        go mod tidy

## Configuração

Certifique-se de que os caminhos para os certificados e chaves TLS estão corretos no arquivo client.go:

    const (
            mspID        = "Org1MSP"
            cryptoPath   = "../../test-network/organizations/peerOrganizations/org1.example.com"
            certPath     = cryptoPath + "/users/User1@org1.example.com/msp/signcerts"
            keyPath      = cryptoPath + "/users/User1@org1.example.com/msp/keystore"
            tlsCertPath  = cryptoPath + "/peers/peer0.org1.example.com/tls/ca.crt"
            peerEndpoint = "dns:///localhost:7051"
            gatewayPeer  = "peer0.org1.example.com"
    )

## Compilação e Execução
Para compilar e executar o código, use os seguintes comandos:

    go build -o fabric-client client.go
    ./fabric-client <ação> [opções]

#### Ações Disponíveis:

initLedger: Inicializa o ledger com um conjunto de dados de ativos.

    ./fabric-client initLedger

transferAsset: Transfere a propriedade de um ativo.

    ./fabric-client transferAsset <AssetID> <NovoProprietário>
 
createAsset: Cria um novo ativo no ledger.

    ./fabric-client createAsset <Número>

createAssetBench: Realiza benchmarking para criar ativos a uma taxa específica.

    ./fabric-client createAssetBench <TPS> <Número>

createAssetBenchTime: Realiza benchmarking para criar ativos a uma taxa específica com base no Tempo.

    ./fabric-client createAssetBenchTime <TPS> <Tempo>

createAssetEndorse Cria um novo ativo no ledger, mas com as fases de ordenação, endosso e commit.

    ./fabric-client createAssetEndorse <Número>

createAssetBenchDetailed: Realiza benchmarking para criar ativos a uma taxa específica com as fases de ordenação, endosso e commit

    ./fabric-client createAssetBenchDetailed <TPS> <Número>

createAssetBenchEnd: Realiza benchmarking para criar ativos a uma taxa específica com as fases de ordenação, endosso e commit

    ./fabric-client createAssetBenchEnd <TPS> <Número>

getAllAssets: Retorna todos os ativos atuais no ledger.

     ./fabric-client getAllAssets

readAssetByID: Obtém os detalhes do ativo por ID.

    ./fabric-client readAssetByID <ID>

## Exemplo de Uso

Para inicializar o ledger:

    ./fabric-client initLedger

Para criar um novo ativo:

    ./fabric-client createAsset 10

Para transferir um ativo:

    ./fabric-client transferAsset asset1 JohnDoe

