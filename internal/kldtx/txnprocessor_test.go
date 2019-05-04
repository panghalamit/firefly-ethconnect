// Copyright 2018, 2019 Kaleido

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kldtx

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/spf13/cobra"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/kaleido-io/ethconnect/internal/kldeth"
	"github.com/kaleido-io/ethconnect/internal/kldmessages"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

type errorReply struct {
	status int
	err    error
	txHash string
}

type testTxnContext struct {
	jsonMsg     string
	badMsgType  string
	replies     []kldmessages.ReplyWithHeaders
	errorRepies []*errorReply
}

type testRPC struct {
	ethSendTransactionResult       string
	ethSendTransactionErr          error
	ethGetTransactionCountResult   hexutil.Uint64
	ethGetTransactionCountErr      error
	ethGetTransactionReceiptResult kldeth.TxnReceipt
	ethGetTransactionReceiptErr    error
	calls                          []string
}

const testFromAddr = "0x83dBC8e329b38cBA0Fc4ed99b1Ce9c2a390ABdC1"

var goodDeployTxnJSON = "{" +
	"  \"headers\":{\"type\": \"DeployContract\"}," +
	"  \"solidity\":\"pragma solidity >=0.4.22 <0.6.0; contract t {constructor() public {}}\"," +
	"  \"from\":\"" + testFromAddr + "\"," +
	"  \"nonce\":\"123\"," +
	"  \"gas\":\"123\"" +
	"}"

var goodSendTxnJSON = "{" +
	"  \"headers\":{\"type\": \"SendTransaction\"}," +
	"  \"from\":\"" + testFromAddr + "\"," +
	"  \"gas\":\"123\"," +
	"  \"method\":{\"name\":\"test\"}" +
	"}"

func (r *testRPC) CallContext(ctx context.Context, result interface{}, method string, args ...interface{}) error {
	r.calls = append(r.calls, method)
	if method == "eth_sendTransaction" {
		reflect.ValueOf(result).Elem().Set(reflect.ValueOf(r.ethSendTransactionResult))
		return r.ethSendTransactionErr
	} else if method == "eth_getTransactionCount" {
		reflect.ValueOf(result).Elem().Set(reflect.ValueOf(r.ethGetTransactionCountResult))
		return r.ethGetTransactionCountErr
	} else if method == "eth_getTransactionReceipt" {
		reflect.ValueOf(result).Elem().Set(reflect.ValueOf(r.ethGetTransactionReceiptResult))
		return r.ethGetTransactionReceiptErr
	}
	panic(fmt.Errorf("method unknown to test: %s", method))
}

func (c *testTxnContext) String() string {
	return "<testmessage>"
}

func (c *testTxnContext) Headers() *kldmessages.CommonHeaders {
	commonMsg := kldmessages.RequestCommon{}
	if c.badMsgType != "" {
		commonMsg.Headers.MsgType = c.badMsgType
	} else if err := c.Unmarshal(&commonMsg); err != nil {
		panic(fmt.Errorf("Unable to unmarshal test message: %s", c.jsonMsg))
	}
	log.Infof("Test message headers: %+v", commonMsg.Headers)
	return &commonMsg.Headers
}

func (c *testTxnContext) Unmarshal(msg interface{}) error {
	log.Infof("Unmarshaling test message: %s", c.jsonMsg)
	return json.Unmarshal([]byte(c.jsonMsg), msg)
}

func (c *testTxnContext) SendErrorReply(status int, err error) {
	c.SendErrorReplyWithTX(status, err, "")
}

func (c *testTxnContext) SendErrorReplyWithTX(status int, err error, txHash string) {
	log.Infof("Sending error reply. Status=%d Err=%s", status, err)
	c.errorRepies = append(c.errorRepies, &errorReply{
		status: status,
		err:    err,
		txHash: txHash,
	})
}

func (c *testTxnContext) Reply(replyMsg kldmessages.ReplyWithHeaders) {
	log.Infof("Sending success reply: %s", replyMsg.ReplyHeaders().MsgType)
	c.replies = append(c.replies, replyMsg)
}

func TestOnMessageBadMessage(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{}).(*txnProcessor)
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = "{" +
		"  \"headers\":{\"type\": \"badness\"}" +
		"}"
	txnProcessor.OnMessage(testTxnContext)

	assert.Empty(testTxnContext.replies)
	assert.NotEmpty(testTxnContext.errorRepies)
	assert.Equal(400, testTxnContext.errorRepies[0].status)
	assert.Regexp("Unknown message type", testTxnContext.errorRepies[0].err.Error())
}

func TestOnDeployContractMessageBadMsg(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{}).(*txnProcessor)
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = "{" +
		"  \"headers\":{\"type\": \"DeployContract\"}," +
		"  \"nonce\":\"123\"," +
		"  \"from\":\"0x83dBC8e329b38cBA0Fc4ed99b1Ce9c2a390ABdC1\"" +
		"}"
	txnProcessor.OnMessage(testTxnContext)

	assert.NotEmpty(testTxnContext.errorRepies)
	assert.Empty(testTxnContext.replies)
	assert.Equal("Missing Compliled Code + ABI, or Solidity", testTxnContext.errorRepies[0].err.Error())

}
func TestOnDeployContractMessageBadJSON(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{}).(*txnProcessor)
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = "badness"
	testTxnContext.badMsgType = kldmessages.MsgTypeDeployContract
	txnProcessor.OnMessage(testTxnContext)

	assert.NotEmpty(testTxnContext.errorRepies)
	assert.Empty(testTxnContext.replies)
	assert.Regexp("invalid character", testTxnContext.errorRepies[0].err.Error())

}
func TestOnDeployContractMessageGoodTxnErrOnReceipt(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{
		MaxTXWaitTime: 1,
	}).(*txnProcessor)
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = goodDeployTxnJSON
	testRPC := &testRPC{
		ethSendTransactionResult:    "0xac18e98664e160305cdb77e75e5eae32e55447e94ad8ceb0123729589ed09f8b",
		ethGetTransactionReceiptErr: fmt.Errorf("pop"),
	}
	txnProcessor.Init(testRPC)                          // configured in seconds for real world
	txnProcessor.maxTXWaitTime = 250 * time.Millisecond // ... but fail asap for this test

	txnProcessor.OnMessage(testTxnContext)
	txnWG := &txnProcessor.inflightTxns[strings.ToLower(testFromAddr)][0].wg

	txnWG.Wait()
	assert.Equal(1, len(testTxnContext.errorRepies))

	assert.Equal("eth_sendTransaction", testRPC.calls[0])
	assert.Equal("eth_getTransactionReceipt", testRPC.calls[1])

	assert.Regexp("Error obtaining transaction receipt", testTxnContext.errorRepies[0].err.Error())

}

func goodMessageRPC() *testRPC {
	blockHash := common.HexToHash("0x6e710868fd2d0ac1f141ba3f0cd569e38ce1999d8f39518ee7633d2b9a7122af")
	blockNumber := hexutil.Big(*big.NewInt(12345))
	contractAddr := common.HexToAddress("0x28a62Cb478a3c3d4DAAD84F1148ea16cd1A66F37")
	cumulativeGasUsed := hexutil.Big(*big.NewInt(23456))
	fromAddr := common.HexToAddress("0xBa25be62a5C55d4ad1d5520268806A8730A4DE5E")
	gasUsed := hexutil.Big(*big.NewInt(345678))
	status := hexutil.Big(*big.NewInt(1))
	toAddr := common.HexToAddress("0xD7FAC2bCe408Ed7C6ded07a32038b1F79C2b27d3")
	transactionHash := common.HexToHash("0xe2215336b09f9b5b82e36e1144ed64f40a42e61b68fdaca82549fd98b8531a89")
	transactionIndex := hexutil.Uint(456789)
	testRPC := &testRPC{
		ethSendTransactionResult: transactionHash.String(),
		ethGetTransactionReceiptResult: kldeth.TxnReceipt{
			BlockHash:         &blockHash,
			BlockNumber:       &blockNumber,
			ContractAddress:   &contractAddr,
			CumulativeGasUsed: &cumulativeGasUsed,
			From:              &fromAddr,
			GasUsed:           &gasUsed,
			Status:            &status,
			To:                &toAddr,
			TransactionHash:   &transactionHash,
			TransactionIndex:  &transactionIndex,
		},
	}
	return testRPC
}

func TestOnDeployContractMessageGoodTxnMined(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{
		MaxTXWaitTime: 1,
	}).(*txnProcessor)
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = goodDeployTxnJSON

	testRPC := goodMessageRPC()
	txnProcessor.Init(testRPC)                          // configured in seconds for real world
	txnProcessor.maxTXWaitTime = 250 * time.Millisecond // ... but fail asap for this test

	txnProcessor.OnMessage(testTxnContext)
	txnWG := &txnProcessor.inflightTxns[strings.ToLower(testFromAddr)][0].wg

	txnWG.Wait()
	assert.Equal(0, len(testTxnContext.errorRepies))

	assert.Equal("eth_sendTransaction", testRPC.calls[0])
	assert.Equal("eth_getTransactionReceipt", testRPC.calls[1])

	replyMsg := testTxnContext.replies[0]
	assert.Equal("TransactionSuccess", replyMsg.ReplyHeaders().MsgType)
	replyMsgBytes, _ := json.Marshal(&replyMsg)
	var replyMsgMap map[string]interface{}
	json.Unmarshal(replyMsgBytes, &replyMsgMap)

	assert.Equal("0x6e710868fd2d0ac1f141ba3f0cd569e38ce1999d8f39518ee7633d2b9a7122af", replyMsgMap["blockHash"])
	assert.Equal("12345", replyMsgMap["blockNumber"])
	assert.Equal("0x28a62cb478a3c3d4daad84f1148ea16cd1a66f37", replyMsgMap["contractAddress"])
	assert.Equal("23456", replyMsgMap["cumulativeGasUsed"])
	assert.Equal("0xba25be62a5c55d4ad1d5520268806a8730a4de5e", replyMsgMap["from"])
	assert.Equal("345678", replyMsgMap["gasUsed"])
	assert.Equal("123", replyMsgMap["nonce"])
	assert.Equal("1", replyMsgMap["status"])
	assert.Equal("0xd7fac2bce408ed7c6ded07a32038b1f79c2b27d3", replyMsgMap["to"])
	assert.Equal("456789", replyMsgMap["transactionIndex"])
}

func TestOnDeployContractMessageGoodTxnMinedWithHex(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{
		MaxTXWaitTime:      1,
		HexValuesInReceipt: true,
	}).(*txnProcessor)
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = goodDeployTxnJSON

	testRPC := goodMessageRPC()
	txnProcessor.Init(testRPC)                          // configured in seconds for real world
	txnProcessor.maxTXWaitTime = 250 * time.Millisecond // ... but fail asap for this test

	txnProcessor.OnMessage(testTxnContext)
	txnWG := &txnProcessor.inflightTxns[strings.ToLower(testFromAddr)][0].wg

	txnWG.Wait()
	assert.Equal(0, len(testTxnContext.errorRepies))

	assert.Equal("eth_sendTransaction", testRPC.calls[0])
	assert.Equal("eth_getTransactionReceipt", testRPC.calls[1])

	replyMsg := testTxnContext.replies[0]
	assert.Equal("TransactionSuccess", replyMsg.ReplyHeaders().MsgType)
	replyMsgBytes, _ := json.Marshal(&replyMsg)
	var replyMsgMap map[string]interface{}
	json.Unmarshal(replyMsgBytes, &replyMsgMap)

	assert.Equal("0x6e710868fd2d0ac1f141ba3f0cd569e38ce1999d8f39518ee7633d2b9a7122af", replyMsgMap["blockHash"])
	assert.Equal("12345", replyMsgMap["blockNumber"])
	assert.Equal("0x3039", replyMsgMap["blockNumberHex"])
	assert.Equal("0x28a62cb478a3c3d4daad84f1148ea16cd1a66f37", replyMsgMap["contractAddress"])
	assert.Equal("23456", replyMsgMap["cumulativeGasUsed"])
	assert.Equal("0x5ba0", replyMsgMap["cumulativeGasUsedHex"])
	assert.Equal("0xba25be62a5c55d4ad1d5520268806a8730a4de5e", replyMsgMap["from"])
	assert.Equal("345678", replyMsgMap["gasUsed"])
	assert.Equal("0x5464e", replyMsgMap["gasUsedHex"])
	assert.Equal("123", replyMsgMap["nonce"])
	assert.Equal("0x7b", replyMsgMap["nonceHex"])
	assert.Equal("1", replyMsgMap["status"])
	assert.Equal("0x1", replyMsgMap["statusHex"])
	assert.Equal("0xd7fac2bce408ed7c6ded07a32038b1f79c2b27d3", replyMsgMap["to"])
	assert.Equal("456789", replyMsgMap["transactionIndex"])
	assert.Equal("0x6f855", replyMsgMap["transactionIndexHex"])
}

func TestOnDeployContractMessageFailedTxnMined(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{
		MaxTXWaitTime: 1,
	}).(*txnProcessor)
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = goodDeployTxnJSON

	testRPC := goodMessageRPC()
	failStatus := hexutil.Big(*big.NewInt(0))
	testRPC.ethGetTransactionReceiptResult.Status = &failStatus
	txnProcessor.Init(testRPC)                          // configured in seconds for real world
	txnProcessor.maxTXWaitTime = 250 * time.Millisecond // ... but fail asap for this test

	txnProcessor.OnMessage(testTxnContext)
	txnWG := &txnProcessor.inflightTxns[strings.ToLower(testFromAddr)][0].wg

	txnWG.Wait()
	replyMsg := testTxnContext.replies[0]
	assert.Equal("TransactionFailure", replyMsg.ReplyHeaders().MsgType)
}

func TestOnDeployContractMessageFailedTxn(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{
		MaxTXWaitTime: 5000,
	}).(*txnProcessor)
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = goodDeployTxnJSON
	testRPC := &testRPC{
		ethSendTransactionErr: fmt.Errorf("fizzle"),
	}
	txnProcessor.Init(testRPC)

	txnProcessor.OnMessage(testTxnContext)

	assert.Equal("fizzle", testTxnContext.errorRepies[0].err.Error())
	assert.EqualValues([]string{"eth_sendTransaction"}, testRPC.calls)
}

func TestOnDeployContractMessageFailedToGetNonce(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{
		MaxTXWaitTime: 1,
	}).(*txnProcessor)
	txnProcessor.conf.PredictNonces = true
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = "{" +
		"  \"headers\":{\"type\": \"DeployContract\"}," +
		"  \"from\":\"0x83dBC8e329b38cBA0Fc4ed99b1Ce9c2a390ABdC1\"" +
		"}"
	testRPC := &testRPC{
		ethGetTransactionCountErr: fmt.Errorf("ding"),
	}
	txnProcessor.Init(testRPC)

	txnProcessor.OnMessage(testTxnContext)

	assert.Equal("ding", testTxnContext.errorRepies[0].err.Error())
	assert.EqualValues([]string{"eth_getTransactionCount"}, testRPC.calls)
}

func TestOnSendTransactionMessageMissingFrom(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{}).(*txnProcessor)
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = "{" +
		"  \"headers\":{\"type\": \"SendTransaction\"}," +
		"  \"nonce\":\"123\"" +
		"}"
	txnProcessor.OnMessage(testTxnContext)

	assert.NotEmpty(testTxnContext.errorRepies)
	assert.Empty(testTxnContext.replies)
	assert.Regexp("'from' must be supplied", testTxnContext.errorRepies[0].err.Error())

}

func TestOnSendTransactionMessageBadNonce(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{}).(*txnProcessor)
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = "{" +
		"  \"headers\":{\"type\": \"SendTransaction\"}," +
		"  \"from\":\"0x83dBC8e329b38cBA0Fc4ed99b1Ce9c2a390ABdC1\"," +
		"  \"nonce\":\"abc\"" +
		"}"
	txnProcessor.OnMessage(testTxnContext)

	assert.NotEmpty(testTxnContext.errorRepies)
	assert.Empty(testTxnContext.replies)
	assert.Regexp("Converting supplied 'nonce' to integer", testTxnContext.errorRepies[0].err.Error())

}

func TestOnSendTransactionMessageBadMsg(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{}).(*txnProcessor)
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = "{" +
		"  \"headers\":{\"type\": \"SendTransaction\"}," +
		"  \"from\":\"0x83dBC8e329b38cBA0Fc4ed99b1Ce9c2a390ABdC1\"," +
		"  \"nonce\":\"123\"," +
		"  \"value\":\"abc\"," +
		"  \"method\":{\"name\":\"test\"}" +
		"}"
	txnProcessor.OnMessage(testTxnContext)

	assert.NotEmpty(testTxnContext.errorRepies)
	assert.Empty(testTxnContext.replies)
	assert.Regexp("Converting supplied 'value' to big integer", testTxnContext.errorRepies[0].err.Error())

}

func TestOnSendTransactionMessageBadJSON(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{}).(*txnProcessor)
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = "badness"
	testTxnContext.badMsgType = kldmessages.MsgTypeSendTransaction
	txnProcessor.OnMessage(testTxnContext)

	assert.NotEmpty(testTxnContext.errorRepies)
	assert.Empty(testTxnContext.replies)
	assert.Regexp("invalid character", testTxnContext.errorRepies[0].err.Error())

}

func TestOnSendTransactionMessageTxnTimeout(t *testing.T) {
	assert := assert.New(t)

	txHash := "0xac18e98664e160305cdb77e75e5eae32e55447e94ad8ceb0123729589ed09f8b"
	txnProcessor := NewTxnProcessor(&TxnProcessorConf{
		MaxTXWaitTime: 1,
	}).(*txnProcessor)
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = goodSendTxnJSON
	testRPC := &testRPC{
		ethSendTransactionResult: txHash,
	}
	txnProcessor.Init(testRPC)                          // configured in seconds for real world
	txnProcessor.maxTXWaitTime = 250 * time.Millisecond // ... but fail asap for this test

	txnProcessor.OnMessage(testTxnContext)
	txnWG := &txnProcessor.inflightTxns[strings.ToLower(testFromAddr)][0].wg
	txnWG.Wait()
	assert.Equal(1, len(testTxnContext.errorRepies))

	assert.Equal("eth_sendTransaction", testRPC.calls[0])
	assert.Equal("eth_getTransactionReceipt", testRPC.calls[1])

	assert.Regexp("Timed out waiting for transaction receipt", testTxnContext.errorRepies[0].err.Error())
	assert.Equal(txHash, testTxnContext.errorRepies[0].txHash)

}

func TestOnSendTransactionMessageFailedTxn(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{
		MaxTXWaitTime: 1,
	}).(*txnProcessor)
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = goodSendTxnJSON
	testRPC := &testRPC{
		ethSendTransactionErr: fmt.Errorf("pop"),
	}
	txnProcessor.Init(testRPC)

	txnProcessor.OnMessage(testTxnContext)

	assert.Equal("pop", testTxnContext.errorRepies[0].err.Error())
	assert.EqualValues([]string{"eth_sendTransaction"}, testRPC.calls)
}

func TestOnSendTransactionMessageFailedToGetNonce(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{
		MaxTXWaitTime: 1,
	}).(*txnProcessor)
	txnProcessor.conf.PredictNonces = true
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = "{" +
		"  \"headers\":{\"type\": \"SendTransaction\"}," +
		"  \"from\":\"0x83dBC8e329b38cBA0Fc4ed99b1Ce9c2a390ABdC1\"" +
		"}"
	testRPC := &testRPC{
		ethGetTransactionCountErr: fmt.Errorf("poof"),
	}
	txnProcessor.Init(testRPC)

	txnProcessor.OnMessage(testTxnContext)

	assert.Equal("poof", testTxnContext.errorRepies[0].err.Error())
	assert.EqualValues([]string{"eth_getTransactionCount"}, testRPC.calls)
}

func TestOnSendTransactionMessageInflightNonce(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{
		MaxTXWaitTime: 1,
	}).(*txnProcessor)
	txnProcessor.inflightTxns["0x83dbc8e329b38cba0fc4ed99b1ce9c2a390abdc1"] =
		[]*inflightTxn{&inflightTxn{nonce: 100}, &inflightTxn{nonce: 101}}
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = "{" +
		"  \"headers\":{\"type\": \"SendTransaction\"}," +
		"  \"from\":\"0x83dBC8e329b38cBA0Fc4ed99b1Ce9c2a390ABdC1\"," +
		"  \"gas\":\"123\"," +
		"  \"method\":{\"name\":\"test\"}" +
		"}"
	testRPC := &testRPC{
		ethSendTransactionResult: "0xac18e98664e160305cdb77e75e5eae32e55447e94ad8ceb0123729589ed09f8b",
	}
	txnProcessor.Init(testRPC)

	txnProcessor.OnMessage(testTxnContext)

	assert.Empty(testTxnContext.errorRepies)
	assert.EqualValues([]string{"eth_sendTransaction"}, testRPC.calls)
}

func TestCobraInitTxnProcessor(t *testing.T) {
	assert := assert.New(t)
	txconf := &TxnProcessorConf{}
	cmd := &cobra.Command{}
	CobraInitTxnProcessor(cmd, txconf)
	cmd.ParseFlags([]string{
		"-x", "10",
		"-P",
	})
	assert.Equal(10, txconf.MaxTXWaitTime)
	assert.Equal(true, txconf.PredictNonces)
}