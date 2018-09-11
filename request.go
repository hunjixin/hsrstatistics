package main
import (
	"strings"
	"errors"
	"encoding/json"
	"fmt"
)
func GetBlockHash(height uint64) (string, error){
	req, err := NewRequest("getblockhash",[]interface{}{height})
	reqbytes, err := json.Marshal(req)
	if err != nil {
		return "", errors.New("请求序列化失败:" + err.Error())
	}

	resBody, err := sendPostRequest(reqbytes)
	if err != nil {
		return "", errors.New("请求节点失败:" + err.Error())
	}
	res := Resp{}
	err = json.Unmarshal(resBody,&res)
	if err != nil  {
		return "", errors.New("响应序列化失败:" + err.Error()) 
	}
	if res.Error != nil {
		return "", errors.New(res.Error.Message) 
	}
	
	return strings.Replace(string(res.Result), "\"", "", -1), nil
}
func GetBlockCount() (int64, error){
	req, err := NewRequest("getblockcount",[]interface{}{})
	reqbytes, err := json.Marshal(req)
	if err != nil {
		return 0, errors.New("请求序列化失败:" + err.Error())
	}

	resBody, err := sendPostRequest(reqbytes)
	if err != nil {
		return 0, errors.New("请求节点失败:" + err.Error())
	}
	res := Resp{}
	err = json.Unmarshal(resBody,&res)
	if err != nil  {
		return 0, errors.New("响应序列化失败:" + err.Error()) 
	}
	if res.Error != nil {
		return 0, errors.New(res.Error.Message) 
	}
	count := int64(0)
	err = json.Unmarshal(res.Result, &count)
	if err != nil  {
		return 0, errors.New("响应序列化失败:" + err.Error()) 
	}
	return count, nil
}
func GetBlock(blockHash string) (*GetBlockVerboseResult, error){
	req, err := NewRequest("getblock",[]interface{}{blockHash,true})
	reqbytes, err := json.Marshal(req)
	if err != nil {
		return nil, errors.New("请求序列化失败:" + err.Error())
	}

	resBody, err := sendPostRequest(reqbytes)
	if err != nil {
		return nil, errors.New("请求节点失败:" + err.Error())
	}
	res := Resp{}
	err = json.Unmarshal(resBody,&res)
	if err != nil  {
		return nil, errors.New("响应序列化失败:" + err.Error()) 
	}
	if res.Error != nil {
		return nil, errors.New(res.Error.Message) 
	}
	block := GetBlockVerboseResult{}
	err = json.Unmarshal(res.Result, &block)
	if err != nil  {
		fmt.Println(err.Error())
		return nil, errors.New("响应序列化失败:" + err.Error()) 
	}
	return &block, nil
}

func GetBlockByHeght(height uint64) (*GetBlockVerboseResult, error){

	blockHash, err := GetBlockHash(height)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("获取区块hash失败：%d   msg: %s", height, err.Error()))
	}

	block, err := GetBlock(blockHash)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("获取区块失败：%d   msg: %s", height, err.Error()))
	}
	return block, nil
}

func GetBatchBlockHash(heights []uint64) ([]string, error){
	reqs := []*Request{}
	for _, height := range heights {
		getBlockHashRequest, err := NewRequest("getblockhash",[]interface{}{height})
		if err != nil {
			return nil, errors.New("请求序列化失败:" + err.Error())
		}
		reqs = append(reqs,getBlockHashRequest)
	}

	
	reqbytes, err := json.Marshal(reqs)
	resBody, err := sendPostRequest(reqbytes)
	if err != nil {
		return nil, errors.New("请求节点数据失败:" + err.Error())
	}

	reses := []Resp{}
	err = json.Unmarshal(resBody,&reses)
	if err != nil {
		return nil, errors.New("响应序列化失败:" + err.Error())
	}

	blockHashes := []string{}

	for _, res := range reses {
		hash := ""
		err = json.Unmarshal(res.Result,&hash)
		if err != nil {
			return nil, errors.New("响应序列化失败:" + err.Error())
		}
		blockHashes = append(blockHashes,hash)
	}
	return blockHashes, nil
}


func GetBatchBlock(hashes []string) ([]*GetBlockVerboseResult, error){
	reqs := []*Request{}
	for _, hash := range hashes {
		getBlockHashRequest, err := NewRequest("getblock",[]interface{}{hash,true})
		if err != nil {
			return nil, errors.New("请求序列化失败:" + err.Error())
		}
		reqs = append(reqs,getBlockHashRequest)
	}
	reqbytes, err := json.Marshal(reqs)
	resBody, err := sendPostRequest(reqbytes)
	if err != nil {
		return nil, errors.New("请求节点数据失败:" + err.Error())
	}

	reses := []Resp{}
	err = json.Unmarshal(resBody,&reses)
	if err != nil {
		return nil, errors.New("响应序列化失败:" + err.Error())
	}

	blocks := []*GetBlockVerboseResult{}
	for _, res := range reses {
		block := &GetBlockVerboseResult{}
		err = json.Unmarshal(res.Result,&block)
		if err != nil {
			return nil, errors.New("响应序列化失败:" + err.Error())
		}
		blocks = append(blocks,block)
	}
	return blocks, nil
}
