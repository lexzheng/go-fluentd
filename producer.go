package concator

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/kataras/iris"

	"go.uber.org/zap"

	utils "github.com/Laisky/go-utils"
)

// Producer send messages to downstream
type Producer struct {
	addr               string
	msgChan            <-chan *FluentMsg
	msgPool            *sync.Pool
	producerTagChanMap map[string]chan<- *FluentMsg
	retryMsgChan       chan *FluentMsg
}

// NewProducer create new producer
func NewProducer(addr string, msgChan <-chan *FluentMsg, msgPool *sync.Pool) *Producer {
	utils.Logger.Info("create Producer")
	p := &Producer{
		addr:               addr,
		msgChan:            msgChan,
		msgPool:            msgPool,
		producerTagChanMap: map[string]chan<- *FluentMsg{},
		retryMsgChan:       make(chan *FluentMsg, 500),
	}
	p.BindMonitor()
	return p
}

// BindMonitor bind monitor for producer
func (p *Producer) BindMonitor() {
	utils.Logger.Info("bind `/monitor/producer`")
	Server.Get("/monitor/producer", func(ctx iris.Context) {
		cnt := "producerTagChanMap tag:chan\n"
		for tag, c := range p.producerTagChanMap {
			cnt += fmt.Sprintf("> %v: %v\n", tag, len(c))
		}
		cnt += fmt.Sprintf("> retryMsgChan: %v\n", len(p.retryMsgChan))
		ctx.Writef(cnt)
	})
}

// Run starting <n> Producer to send messages
func (p *Producer) Run(fork int, commitChan chan<- int64) {
	utils.Logger.Info("start producer", zap.String("addr", p.addr))

	var (
		msg *FluentMsg
		ok  bool
	)

	for {
		select {
		case msg = <-p.retryMsgChan:
		case msg = <-p.msgChan:
		}

		if _, ok = p.producerTagChanMap[msg.Tag]; !ok {
			p.producerTagChanMap[msg.Tag] = p.SpawnForTag(fork, msg.Tag, commitChan)
		}

		select {
		case p.producerTagChanMap[msg.Tag] <- msg:
		default:
		}
	}

}

// SpawnForTag spawn `fork` numbers connections to downstream for each tag
func (p *Producer) SpawnForTag(fork int, tag string, commitChan chan<- int64) chan<- *FluentMsg {
	utils.Logger.Info("SpawnForTag", zap.Int("fork", fork), zap.String("tag", tag))
	var (
		inChan = make(chan *FluentMsg, 1000) // for each tag
	)

	for i := 0; i < fork; i++ { // parallel to each tag
		go func() {
			defer utils.Logger.Error("producer exits", zap.String("tag", tag))

			var (
				nRetry           = 0
				maxRetry         = 3
				id               int64
				msg              *FluentMsg
				maxNBatch        = utils.Settings.GetInt("settings.msg_batch_size")
				msgBatch         = make([]*FluentMsg, maxNBatch)
				msgBatchDelivery []*FluentMsg
				iBatch           = 0
				lastT            = time.Unix(0, 0)
				maxWait          = 30 * time.Second
				encoder          *Encoder
			)

		RECONNECT: // reconnect to downstream
			conn, err := net.DialTimeout("tcp", p.addr, 10*time.Second)
			if err != nil {
				utils.Logger.Error("try to connect to backend got error", zap.Error(err), zap.String("tag", tag))
				time.Sleep(1 * time.Second)
				goto RECONNECT
			}
			utils.Logger.Info("connected to backend",
				zap.String("backend", conn.RemoteAddr().String()),
				zap.String("tag", tag))

			encoder = NewEncoder(conn) // one encoder for each connection
			for msg = range inChan {
				msgBatch[iBatch] = msg
				iBatch++
				if iBatch < maxNBatch &&
					time.Now().Sub(lastT) < maxWait {
					continue
				}
				lastT = time.Now()
				msgBatchDelivery = msgBatch[:iBatch]
				iBatch = 0

				nRetry = 0
				for {
					if utils.Settings.GetBool("dry") {
						utils.Logger.Info("send message to backend",
							zap.String("tag", tag),
							zap.String("log", fmt.Sprintf("%v", msgBatch[0].Message)))
						goto FINISHED
					}

					if err = encoder.EncodeBatch(tag, msgBatchDelivery); err != nil {
						nRetry++
						if nRetry > maxRetry {
							utils.Logger.Error("try send message got error", zap.Error(err), zap.String("tag", tag))

							for _, msg = range msgBatchDelivery {
								p.retryMsgChan <- msg
							}

							if err = conn.Close(); err != nil {
								utils.Logger.Error("try to close connection got error", zap.Error(err))
							}
							utils.Logger.Info("connection closed, try to reconnect...")
							goto RECONNECT
						}

						time.Sleep(100 * time.Microsecond)
						continue
					}

					utils.Logger.Debug("success sent message to backend", zap.String("backend", p.addr), zap.String("tag", tag))
					goto FINISHED
				}

			FINISHED:
				for _, msg = range msgBatchDelivery {
					commitChan <- msg.Id
					if msg.extIds != nil {
						for _, id = range msg.extIds {
							commitChan <- id
						}
					}

					msg.extIds = nil
					p.msgPool.Put(msg)
				}
			}
		}()
	}

	return inChan
}