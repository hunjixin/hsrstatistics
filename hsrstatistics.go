package main

import (
	"time"
	"github.com/shopspring/decimal"
	"os"
	"errors"
	"sync"
	//"net/http"

	"runtime"
	"runtime/pprof"
	_ "net/http/pprof"
	"fmt"
	"encoding/json"
	"github.com/jinzhu/gorm"

	_ "github.com/jinzhu/gorm/dialects/mysql"
)


type OutPutCache struct {
	Amount uint64
	AvailableAmount uint64
}

type WriteInfo struct {
	Block	*T_Block
	Uints	[]*InsertUinit
}
var outCache sync.Map//[string]*OutPutCache = make(sync.Map[string]*OutPutCache)

const (
	FIANLHEIGHT = 928888;
	MAX_TRY_TIME = 500
)


func main() {
	runtime.GOMAXPROCS(10)
	//go func() {
    //    http.ListenAndServe("localhost:8080", nil)
    //}()
	connectionString := "root:@tcp(127.0.0.1)/hsr_nanshan_db?charset=utf8&parseTime=True&loc=Local"
	
	db, err := gorm.Open("mysql", connectionString)
	db.Exec("SET GLOBAL TRANSACTION ISOLATION LEVEL READ UNCOMMITTED;")
	db.Exec("set global innodb_lock_wait_timeout=100; ")
	if err != nil {
		panic("连接数据库失败")
	}
	defer db.Close()
	//构建数据库
	ConstructTables(db)
	//高度搜索
	hasSavedHeight, err := GetHasSaveHeight(db)
	if err != nil { return }
	// 缓存重建
	BuildCache(db)

	blockQueues := make(chan []*GetBlockVerboseResult, 80)
	flushMessageQueues := make(chan []*WriteInfo, 10) // make([]*InsertUinit, 0)
	saveNum := 10;
	saveProcessor := make(chan *struct{}, saveNum)  //数据库并发连接控制
	for i := 0; i < saveNum; i++ {
		saveProcessor <- &struct{}{}
	}

	errChanel := make(chan *struct{})
	stopProcessor := make(chan *struct{})
	stopSaver := make(chan *struct{})
	go (func(){
		go func(){
			for {
				select {
					case <-stopProcessor:
						return;
					default:
						log.Infof("块缓存长度：%v  处理后待写入缓存长度: %v   正在写入的连接：%v", len(blockQueues), len(flushMessageQueues), saveNum - len(saveProcessor))
						time.Sleep(10 * time.Second)
				}
			}
		}()
		go func(){
			for {
				select {
					case <-errChanel:
						stopProcessor <- &struct{}{}
						stopSaver <- &struct{}{}
						return;
					default:
						time.Sleep(100 * time.Millisecond)
				}
			}
		}()

		go func(){
			for {
				select {
					case blocks :=<- blockQueues:
						ProcessBlocks(db, blocks, flushMessageQueues, errChanel)
					case <-stopProcessor:
						return;
					default:
						time.Sleep(100 * time.Millisecond)
				}
			}
		}()

		go func(){
			for {
				select {
					case writeInfo :=<- flushMessageQueues:
						<- saveProcessor
						go func(){
							mErr := FlushBatches(db, writeInfo)
							if mErr != nil {
								errChanel <- &struct{}{}
								return
							}
							saveProcessor <- &struct{}{}
						}()
						
					case <-stopSaver:
						return;
					default:
						time.Sleep(100 * time.Millisecond)
				}
			}
		}()
	})()

	recoverHieghts, err := Recover(db)
	
	if err == nil {
		/*for _,height := range recoverHieghts {
			block, err := GetBlockByHeght(height)
			if err != nil {
				log.Error("Recover 获取区块失败:" + err.Error())
				return
			}
			PreProcessBlock(block)
			writeInfo, err := ProcessBlock(block)
			if err != nil {
				log.Error("Recover "+err.Error())
				return
			}
			if writeInfo != nil {
				err, _, _, _ = FlushSingle(db,	writeInfo)
				if err != nil {
					log.Error("Recover "+err.Error())
					return
				}
			}
			log.Info("Recover 恢复高度 %v", height)
		}*/
		if len(recoverHieghts) >0 {
			log.Infof("需要重新恢复的区块数量：%d", len(recoverHieghts))
			blocks, err := GetBatchBlocks(recoverHieghts)
			if err != nil {
				log.Error("Recover 获取区块失败:" + err.Error())
				return
			}
			blockQueues <- blocks
			log.Info("故障恢复成功")
		}
	} else {
		log.Error("Recover检查失败")
		return
	}
	//扫描区块
	latestHeight, err := GetBlockCount()
	latestHeight = latestHeight -5
	if err != nil {
		log.Info("获取最新区块id失败")
		return 
	}

	//任务切分
	type TaskEntry struct {
		StartHeight uint64  //包含
 		EndHeight uint64	//不包含
	}

	requestPerBatch := 200
	tasks := make([]*TaskEntry, 0)
	var taskNum = int((int(latestHeight) - int(hasSavedHeight)) / requestPerBatch)
	for i := 0; i < taskNum; i++ {
		tasks = append(tasks, &TaskEntry{
			StartHeight: hasSavedHeight + uint64(requestPerBatch * i + 1),
			EndHeight:	hasSavedHeight + uint64(requestPerBatch * (i + 1) + 1),
		})
	}
	for i := 0; i < len(tasks); {
		batchNum := 50
		var getBlockGroup sync.WaitGroup
		getBlockGroup.Add(batchNum)
		requestBlocks := [][]*GetBlockVerboseResult{}
		for j := 0; j < batchNum; j++ { 
			go func(closureTask *TaskEntry){
				defer func(){
					getBlockGroup.Done()
				}()

				tryTime := 0
				for {
					if closureTask.EndHeight > uint64(latestHeight) {
						closureTask.EndHeight = uint64(latestHeight) + 1
					}
	
					blocks, err := GetBatchBlocksFromTo(closureTask.StartHeight, closureTask.EndHeight)
					if err != nil {
						if tryTime < MAX_TRY_TIME {
							log.Error("重试  请求区块数据失败" + err.Error())
							time.Sleep(time.Second * 5)
							tryTime++
							continue
						} else {
							log.Error("请求区块数据失败" + err.Error())
							errChanel <- &struct{}{}
							return
						}
					}
					requestBlocks = append(requestBlocks, blocks)
					break
				}
			
			}(tasks[i + j])
		}

		getBlockGroup.Wait()
		for _, blocks := range requestBlocks {
			blockQueues <- blocks	
		}
		i += batchNum
	}

	//同步更新
    for {
		newHeight, _ := GetBlockCount() 
		for i := latestHeight; i < newHeight + 1 -5; i++ {
			block, _ := GetBlockByHeght(uint64(i))
			writeInfo, _ := ProcessBlock(block)
			FlushSingle(db, writeInfo)
		}
		latestHeight = newHeight + 1
	}
}
func MarkSpent(db *gorm.DB){
	allVin := []*T_Vin{}
	allVout := []*T_Vout{}
	db.Find(&allVin)
	db.Find(&allVout)
	
	
}
func GetBatchBlocksFromTo(startHeight uint64, endHeight uint64) ([]*GetBlockVerboseResult, error) {
	//ccc := 1
	heights := []uint64{} 
	for i := startHeight; i < endHeight; i++{
		heights = append(heights, i)
	}
	blockHashes, err := GetBatchBlockHash(heights)
	if err != nil {
		log.Errorf("获取区块hash失败 msg: %s", err.Error())
		return nil, errors.New(fmt.Sprintf("获取区块hash失败 msg: %s", err.Error()))
	}

	blocks, err := GetBatchBlock(blockHashes)
	if err != nil {
		log.Errorf("获取区块失败   msg: %s", err.Error())
		return nil, errors.New(fmt.Sprintf("获取区块失败   msg: %s", err.Error()))
	}
	PreProcessBlocks(blocks)
	return blocks, nil
}
func GetBatchBlocks(heights []uint64) ([]*GetBlockVerboseResult, error) {
	blockHashes, err := GetBatchBlockHash(heights)
	if err != nil {
		log.Errorf("获取区块hash失败 msg: %s", err.Error())
		return nil, errors.New(fmt.Sprintf("获取区块hash失败 msg: %s", err.Error()))
	}

	blocks, err := GetBatchBlock(blockHashes)
	if err != nil {
		log.Errorf("获取区块失败   msg: %s", err.Error())
		return nil, errors.New(fmt.Sprintf("获取区块失败   msg: %s", err.Error()))
	}
	PreProcessBlocks(blocks)
	return blocks, nil
}

func PreProcessBlocks(blocks []*GetBlockVerboseResult){
	for _, block := range blocks {
		PreProcessBlock(block)
	}
}

func PreProcessBlock(block *GetBlockVerboseResult){
	//block.RawTx = FilterNoNValue(block.RawTx)
	for _, raw_transaction :=range block.RawTx {
		for _, raw_vout :=range raw_transaction.Vout {
			outCache.Store(raw_transaction.Txid +fmt.Sprint(raw_vout.N), &OutPutCache{
				Amount : uint64(decimal.NewFromFloat(raw_vout.Value).Mul(decimal.NewFromFloat(100000000)).IntPart()),
			})
		}
	}
}

func ProcessBlocks (db *gorm.DB, blocks []*GetBlockVerboseResult, flushMessageQueues chan []*WriteInfo,errChanel chan *struct{}) (uint64,error){
  	writeInfos := make([]*WriteInfo, 0)
	for _, block := range blocks {
		//分析结果
		writeInfo, err := ProcessBlock(block)
		if err != nil {
			log.Error("处理交易失败")
			errChanel <- &struct{}{}
			return 0, err
		}
		if writeInfo != nil {
			writeInfos = append(writeInfos, writeInfo)
		}
	}
	flushMessageQueues <- writeInfos
	log.Debugf("总和 写入交易持久化缓存：%d", len(blocks))
	return uint64(0), nil
}

func ProcessBlock (block *GetBlockVerboseResult) (*WriteInfo, error){
	ius := make([]*InsertUinit, 0)
	if len(block.RawTx) > 0 {
		for _, raw_transaction :=range block.RawTx {
			iu := &InsertUinit{}
			ius = append(ius, iu)
			iu.Transaction = &T_Transaction{} 
			iu.Vins = make([]*T_Vin,0)
			iu.Vouts = make([]*T_Vout,0)
	
			iu.Transaction.Height = uint64(block.Height)
			iu.Transaction.TxId = raw_transaction.Txid
			iu.Transaction.Time = time.Unix(raw_transaction.Time,0)
			iu.Transaction.TxType = GetTransactionType(raw_transaction)
			iu.Transaction.SumIn = GetAllIn(raw_transaction)
			iu.Transaction.SumOut = GetAllOut(raw_transaction)
	
			for _, raw_vin :=range raw_transaction.Vin {
				amount := uint64(0)
				if iu.Transaction.TxType == 0 {
					continue   //coinbase交易 没有vin
				}
				
				if voutCacheEntry, ok := outCache.Load(raw_vin.Txid + fmt.Sprint(raw_vin.Vout)); ok {
					if iu.Transaction.TxType != 0 {
						amount = voutCacheEntry.(*OutPutCache).Amount
					}
					t_vin := &T_Vin{
						TxId : raw_transaction.Txid,
						RefVoutTxId : raw_vin.Txid,
						Height : iu.Transaction.Height,
						Vout : raw_vin.Vout,
						Amount : amount,
					};
					iu.Vins = append(iu.Vins, t_vin)
				} else {
					log.Errorf("输入读取缓存失败 tx: %s  vin: %d", raw_vin.Txid, fmt.Sprint(raw_vin.Vout))
				}
			}
			//查找本交易是pos还是pow  依次填写三个值
			//sort.Reverse(raw_transaction.Vout)
			for _, raw_vout :=range raw_transaction.Vout {
				if raw_vout.ScriptPubKey.Type == "nonstandard" {
					continue
				}
				
				address := ""
				if len(raw_vout.ScriptPubKey.Addresses)>0 {
					address = raw_vout.ScriptPubKey.Addresses[0]
				}
	
				t_vout := &T_Vout {
					TxId     : raw_transaction.Txid,
					Vout     : raw_vout.N,
					Height : iu.Transaction.Height,
					Address  : address,
					Type     : raw_vout.ScriptPubKey.Type,
					Amount   : uint64(decimal.NewFromFloat(raw_vout.Value).Mul(decimal.NewFromFloat(100000000)).IntPart()),
				}
	
				if iu.Transaction.Height > FIANLHEIGHT {
					invalidateValue := uint64(0)
					
					if iu.Transaction.TxType ==1 {
							//输出 - 输入
						invalidateValue = iu.Transaction.SumOut - iu.Transaction.SumIn 
					} else if iu.Transaction.TxType == 2 {
							//输出 - 输入 - 不可用
						invalidateValue = iu.Transaction.SumOut - iu.Transaction.SumIn - GetAllAvailableIn(raw_transaction)
					} else {
						//不可用币
						invalidateValue = iu.Transaction.SumOut - GetAllAvailableIn(raw_transaction)  //手续费算在不可用里面
					}
	
					if invalidateValue != 0  {
						if invalidateValue - t_vout.Amount > 0 {
							t_vout.AvailableAmount = 0
							invalidateValue = invalidateValue - t_vout.Amount
							if invalidateValue < 0 {
								invalidateValue = 0
							}
						} else {
							t_vout.AvailableAmount = t_vout.Amount - uint64(invalidateValue)
							invalidateValue = 0
						}
					}
					
				} else {
					t_vout.AvailableAmount = t_vout.Amount
				}
				t_vout.UnAvailableAmount = t_vout.Amount - t_vout.AvailableAmount
				voutCacheEntry, ok := outCache.Load(raw_transaction.Txid +fmt.Sprint(raw_vout.N))
				if !ok {
					log.Errorf("输出缓存建立失败 tx: %s  vin: %d", raw_transaction.Txid, raw_vout.N)
					return nil, errors.New(fmt.Sprintf("输出缓存建立失败 tx: %s  vin: %d", raw_transaction.Txid, raw_vout.N)) 
				}
				voutCacheEntry.(*OutPutCache).AvailableAmount = t_vout.AvailableAmount
				iu.Vouts =append(iu.Vouts, t_vout)
			}
			log.Debugf("写入交易持久化缓存：%d, 交易id：%s", block.Height, raw_transaction.Txid)
		}
	}

	return &WriteInfo{
		Uints:ius,
		Block:&T_Block{
			Height: uint64(block.Height),
		},
	}, nil
}

func Recover(db *gorm.DB) ([]uint64, error) {
	allBlocks := []T_Block{}
	
	if err := db.Order("height").Find(&allBlocks).Error; err != nil {
		return nil, err
	}
	notRecordBlock := make([]uint64,0)
	if len(allBlocks) == 0 {
		return nil, nil
	}
	preHeight := uint64(0)
	for i:=0; i<len(allBlocks); i++{
		spanHeight :=  allBlocks[i].Height - preHeight
		if spanHeight== 1 {
			preHeight = allBlocks[i].Height
			continue
		}else{
			sHeight := preHeight
			for j:=sHeight + 1; j< sHeight +spanHeight; j++{
				notRecordBlock = append(notRecordBlock, j)
				preHeight = allBlocks[i].Height
			}
		}
	}
	return notRecordBlock, nil
}
//过滤掉无效的coinbase
func FilterNoNValue(rawTxs []TxRawResult)  []TxRawResult{
	newRawTxs := make([]TxRawResult, 0)
	for _ , rawTx := range rawTxs {
		if len(rawTx.Vin)>0 && rawTx.Vin[0].Coinbase != ""  && decimal.NewFromFloat(rawTx.Vout[0].Value).Mul(decimal.NewFromFloat(100000000)).IntPart() ==0 {
			
		} else {
			newRawTxs = append(newRawTxs, rawTx)
		}
	}
	return newRawTxs
}

func GetTransactionType(rawTx TxRawResult)  uint8 {
	if len(rawTx.Vin)>0&&rawTx.Vin[0].Coinbase != "" {
		return 0
	} else if GetAllIn(rawTx)< GetAllOut(rawTx) {
		return 1
	} else{
		return 2
	}
}

func GetAllIn(rawTx TxRawResult) uint64 {
	value := uint64(0);
	for _, raw_in :=range rawTx.Vin {
		if raw_in.Coinbase != "" {
			continue
		}
		if cacheEntry, ok :=  outCache.Load(raw_in.Txid +fmt.Sprint(raw_in.Vout));ok {
			value += cacheEntry.(*OutPutCache).Amount
		} else {
			fmt.Println("coinbase")
		}
	}
	return value
}

func GetAllAvailableIn(rawTx TxRawResult) uint64 {
	value := uint64(0);
	for _, raw_in :=range rawTx.Vin {
		if raw_in.Coinbase != "" {
			continue
		}
		if cacheEntry, ok :=  outCache.Load(raw_in.Txid +fmt.Sprint(raw_in.Vout));ok {
			value += cacheEntry.(*OutPutCache).AvailableAmount
		} else {
			fmt.Println("coinbase")
		}
	}
	return value
}

func GetAllOut(rawTx TxRawResult) uint64 {
	value := uint64(0);
	for _, raw_vout :=range rawTx.Vout {
		value += uint64(decimal.NewFromFloat(raw_vout.Value).Mul(decimal.NewFromFloat(100000000)).IntPart())
	}
	return value
}

func FlushBatches(db *gorm.DB, batchInserts []*WriteInfo) error {
	tx := db.Begin()
	defer func(){
		tx.Commit()
	}()
	alltxCount := 0
	alltxInCount := 0
	alltxOutCount := 0
	for _ , batchInfo := range batchInserts {
		txCount := 0
		txInCount := 0
		txOutCount := 0
		if err := tx.Create(batchInfo.Block).Error; err != nil {
			tx.Rollback()
			return err
		}
		for _ , insertUint := range batchInfo.Uints {

			if err := tx.Create(insertUint.Transaction).Error; err != nil {
				tx.Rollback()
				return err
			}
			txCount++
			for _, vin := range insertUint.Vins {
				if err := tx.Create(vin).Error; err != nil {
					tx.Rollback()
					return err
				}
				prevOUT := &T_Vout{}
				if err := db.Model(&prevOUT).Where("tx_id = ?", vin.RefVoutTxId).Where("vout = ?", vin.Vout).Update("IsSpent",true).Error; err != nil {
					tx.Rollback()
					return err
				}
				txInCount++
			}
		
			
			for _, vout := range insertUint.Vouts {
				if err := tx.Create(vout).Error; err != nil {
					tx.Rollback()
					return err
				}
				txOutCount ++
			}
		}
		alltxCount += txCount
		alltxInCount += txInCount
		alltxOutCount += txOutCount
		log.Debugf("交易写入成功：交易：%llu   输入: %llu    输出：%llu ", txCount, txInCount, txOutCount )
	}
		
	log.Info("总和交易写入成功：%llu 交易：%llu   输入: %llu    输出：%llu ", batchInserts[0].Block.Height, alltxCount, alltxInCount, alltxOutCount )
	return nil
}
func FlushSingle(db *gorm.DB, insert *WriteInfo) (error, int, int, int) {
	tx := db.Begin()
	defer func(){
		tx.Commit()
	}()
	txCount := 0
	txInCount := 0
	txOutCount := 0
	if err := tx.Create(insert.Block).Error; err != nil {
		tx.Rollback()
		return err, 0, 0, 0
	}
	for _ , insertUint := range insert.Uints {

		if err := tx.Create(insertUint.Transaction).Error; err != nil {
			tx.Rollback()
			return err, 0, 0, 0
		}
		txCount++
		for _, vin := range insertUint.Vins { 
			if err := tx.Create(vin).Error; err != nil {
				tx.Rollback()
				return err, 0, 0, 0
			}
			prevOUT := &T_Vout{}
			if err := db.Model(&prevOUT).Where("tx_id = ?", vin.RefVoutTxId).Where("vout = ?", vin.Vout).Update("IsSpent",true).Error; err != nil {
				tx.Rollback()
				return err, 0, 0, 0
			}
			txInCount++
		}
	
		
		for _, vout := range insertUint.Vouts {
			if err := tx.Create(vout).Error; err != nil {
				tx.Rollback()
				return err, 0, 0, 0
			}
			txOutCount ++
		}
	}
	log.Debugf("交易写入成功：交易：%v  输入: %v   输出：%v", txCount, txInCount, txOutCount )


	return nil, txCount, txInCount, txOutCount
}
// 创建请求
func NewRequest(method string, params []interface{}) (*Request, error) {
	rawParams := make([]json.RawMessage, 0, len(params))
	for _, param := range params {
		marshalledParam, err := json.Marshal(param)
		if err != nil {
			return nil, err
		}
		rawMessage := json.RawMessage(marshalledParam)
		rawParams = append(rawParams, rawMessage)
	}

	return &Request{
		Jsonrpc: "2.0",
		Method:  method,
		Params:  rawParams,
	}, nil
}

func BuildCache(db *gorm.DB){
	log.Info("开始构建缓存")
	var outs []T_Vout
	db.Find(&outs)
	for _, out := range outs {
		outCache.Store(out.TxId +fmt.Sprint(out.Vout),&OutPutCache{
			Amount : out.Amount,
			AvailableAmount : out.AvailableAmount ,
		})
	}
	log.Info("缓存创建完成")
}


func GetHasSaveHeight(db *gorm.DB)(uint64,error){
	currentHeight := uint64(0)
	var lastestTransaction T_Transaction

	if err := db.Select("height").Order("height desc").First(&lastestTransaction).Error; err != nil {
		if err.Error() != "record not found" {
			return 0, errors.New(fmt.Sprintf("进度查询错误:" + err.Error()))
		}
	}else{
		currentHeight = lastestTransaction.Height
	}
	
	currentHeight = lastestTransaction.Height
	log.Infof("当前更新高度是：%v", currentHeight)
	return currentHeight, nil
}

func ConstructTables(db *gorm.DB){
	if !db.HasTable(&T_Transaction{}) {
		db.CreateTable(&T_Transaction{})
	}

	if !db.HasTable(&T_Vout{}) { 
		db.CreateTable(&T_Vout{}) 
	}

	if !db.HasTable(&T_Vin{}) {
		db.CreateTable(&T_Vin{})
	}
	
	if !db.HasTable(&T_Block{}) {
		db.CreateTable(&T_Block{})
	}
}
type T_Transaction struct {
    TxId        string 	`gorm:"type:varchar(100);not null;primary_key;"`
	Time      	time.Time	`gorm:"type:timestamp;not null;"`
	TxType      uint8		`gorm:"not null;"`
	Height		uint64 	`gorm:"not null;"`
	SumIn       uint64 	`gorm:"not null;"`
	SumOut      uint64 	`gorm:"not null;"`
}
 
type InsertUinit struct {

	Transaction *T_Transaction

	Vins []*T_Vin

	Vouts []*T_Vout

}

type T_Vin struct {
	TxId       string	`gorm:"type:varchar(100);not null;"`
	RefVoutTxId  string	`gorm:"type:varchar(100);"`
	Height		uint64 	`gorm:"not null;"`
	Vout       uint32	
	Amount     uint64  
}

type T_Vout struct {
	TxId       			string	`gorm:"type:varchar(100);not null;"`
	Vout       			uint32	`gorm:"not null;"`
	Address    			string 	`gorm:"type:varchar(50);not null;"`
	Type    			string 	`gorm:"type:varchar(20);not null;"`
	Height		uint64 	`gorm:"not null;"`
	TxType      		int		`gorm:"not null;"`
	Amount        		uint64 	`gorm:"not null;"`
	UnAvailableAmount   uint64 	`gorm:"not null;"`
	AvailableAmount 	uint64	`gorm:"not null;"`
	IsSpent 			bool	`gorm:"not null;"`
}

type T_Block struct {
    ID     				uint64  `gorm:"primary_key;auto_increment;"`
	Height		uint64 	`gorm:"not null;"`
	Hash       			string	`gorm:"type:varchar(100);not null;"`
}
func trace(i uint64){
	cpuProfile(i)
	//heapProfile(i)
}

// 生成 CPU 报告
func cpuProfile(i uint64) {
	f, err := os.OpenFile("cpu"+fmt.Sprint(i)+".prof", os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
	  log.Error(err.Error())
	}
	defer f.Close()
   
	log.Infof("CPU Profile started")
	pprof.StartCPUProfile(f)
	defer pprof.StopCPUProfile()
   
	time.Sleep(240 * time.Second)
	fmt.Println("CPU Profile stopped")
}
   
// 生成堆内存报告
func heapProfile(i uint64) {
	f, err := os.OpenFile("heap"+fmt.Sprint(i)+".prof", os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		log.Error(err.Error())
	}
	defer f.Close()

	time.Sleep(30 * time.Second)

	pprof.WriteHeapProfile(f)
	fmt.Println("Heap Profile generated")
}