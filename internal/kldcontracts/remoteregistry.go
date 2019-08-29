// Copyright 2019 Kaleido

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kldcontracts

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"

	"github.com/kaleido-io/ethconnect/internal/kldbind"
	"github.com/kaleido-io/ethconnect/internal/kldmessages"
	log "github.com/sirupsen/logrus"
)

const (
	genericRegistryRequestErrorMsg  = "Error querying contract registry"
	genericRegistryResponseErrorMsg = "Error processing contract registry response"
	defaultABIProp                  = "abi"
	defaultBytecodeProp             = "bytecode"
	defaultDevdocProp               = "devdoc"
	defaultDeployableProp           = "deployable"
)

// RemoteRegistry lookup of ABI, ByteCode and DevDocs against a conformant REST API
type RemoteRegistry interface {
	loadFactoryByID(id string) (*kldmessages.DeployContract, error)
	loadFactoryByAddress(addr string) (*kldmessages.DeployContract, error)
}

// RemoteRegistryConf configuration
type RemoteRegistryConf struct {
	FactoryURLPrefix  string                      `json:"factoryURLPrefix"`
	InstanceURLPrefix string                      `json:"instanceURLPrefix"`
	Headers           map[string][]string         `json:"headers"`
	PropNames         RemoteRegistryPropNamesConf `json:"propNames"`
}

// RemoteRegistryPropNamesConf configures the JSON property names to extract from the GET response on the API
type RemoteRegistryPropNamesConf struct {
	ABI        string `json:"abi"`
	Bytecode   string `json:"bytecode"`
	Devdoc     string `json:"devdoc"`
	Deployable string `json:"deployable"`
}

// NewRemoteRegistry construtor
func NewRemoteRegistry(conf *RemoteRegistryConf) RemoteRegistry {
	rr := &remoteRegistry{
		conf: conf,
		client: &http.Client{
			Transport: &http.Transport{
				MaxIdleConns: 1,
			},
		},
	}
	propNames := &conf.PropNames
	if propNames.ABI == "" {
		propNames.ABI = defaultABIProp
	}
	if propNames.Bytecode == "" {
		propNames.Bytecode = defaultBytecodeProp
	}
	if propNames.Devdoc == "" {
		propNames.Devdoc = defaultDevdocProp
	}
	if propNames.Deployable == "" {
		propNames.Deployable = defaultDeployableProp
	}
	if rr.conf.FactoryURLPrefix != "" && !strings.HasSuffix(rr.conf.FactoryURLPrefix, "/") {
		rr.conf.FactoryURLPrefix += "/"
	}
	if rr.conf.InstanceURLPrefix != "" && !strings.HasSuffix(rr.conf.InstanceURLPrefix, "/") {
		rr.conf.InstanceURLPrefix += "/"
	}
	return rr
}

type remoteRegistry struct {
	conf   *RemoteRegistryConf
	client *http.Client
}

func (rr *remoteRegistry) doRequest(method, url string) (map[string]interface{}, error) {
	log.Infof("GET %s -->", url)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header = rr.conf.Headers
	res, err := rr.client.Do(req)
	if err != nil {
		log.Errorf("GET %s <-- !Failed: %s", url, err)
		return nil, fmt.Errorf(genericRegistryRequestErrorMsg)
	}
	log.Infof("GET %s <-- [%d]", url, res.StatusCode)
	if res.StatusCode == 404 {
		return nil, nil
	}
	resBody, err := ioutil.ReadAll(res.Body)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		log.Errorf("GET %s <-- !Failed to ready body: %s", url, err)
		return nil, fmt.Errorf(genericRegistryRequestErrorMsg)
	}
	var jsonBody map[string]interface{}
	if err = json.Unmarshal(resBody, &jsonBody); err != nil {
		log.Errorf("GET %s <-- !Failed to ready body: %s", url, err)
		return nil, fmt.Errorf(genericRegistryResponseErrorMsg)
	}
	return jsonBody, nil
}

func (rr *remoteRegistry) getResponseString(m map[string]interface{}, p string, emptyOK bool) (string, error) {
	genericVal, exists := m[p]
	if !exists {
		return "", fmt.Errorf("'%s' missing in contract registry response", p)
	}
	stringVal, ok := genericVal.(string)
	if !ok {
		return "", fmt.Errorf("'%s' not a string in contract registry response", p)
	}
	if !emptyOK && stringVal == "" {
		return "", fmt.Errorf("'%s' empty in contract registry response", p)
	}
	return stringVal, nil
}

func (rr *remoteRegistry) loadFactoryByID(id string) (*kldmessages.DeployContract, error) {
	if rr.conf.FactoryURLPrefix == "" {
		return nil, nil
	}
	url := rr.conf.FactoryURLPrefix + url.QueryEscape(id)
	jsonRes, err := rr.doRequest("GET", url)
	if err != nil || jsonRes == nil {
		return nil, err
	}
	abiString, err := rr.getResponseString(jsonRes, rr.conf.PropNames.ABI, false)
	if err != nil {
		return nil, err
	}
	var abi *kldbind.ABI
	err = json.Unmarshal([]byte(abiString), &abi)
	if err != nil {
		log.Errorf("GET %s <-- !Failed to decode ABI: %s\n%s", url, err, abiString)
		return nil, fmt.Errorf(genericRegistryResponseErrorMsg)
	}
	devdoc, err := rr.getResponseString(jsonRes, rr.conf.PropNames.Devdoc, true)
	if err != nil {
		return nil, err
	}
	bytecodeStr, err := rr.getResponseString(jsonRes, rr.conf.PropNames.Bytecode, false)
	if err != nil {
		return nil, err
	}
	var bytecode []byte
	if bytecode, err = hex.DecodeString(strings.TrimPrefix(bytecodeStr, "0x")); err != nil {
		log.Errorf("GET %s <-- !Failed to parse bytecode: %s\n%s", url, err, bytecodeStr)
		return nil, fmt.Errorf(genericRegistryResponseErrorMsg)
	}
	return &kldmessages.DeployContract{
		ABI:      abi,
		DevDoc:   devdoc,
		Compiled: bytecode,
	}, nil
}

func (rr *remoteRegistry) loadFactoryByAddress(id string) (*kldmessages.DeployContract, error) {
	return nil, fmt.Errorf("Not implemented")
}