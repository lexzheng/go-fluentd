package libs

import (
	"github.com/Laisky/go-utils"
	"github.com/Laisky/zap"
)

var Logger *utils.LoggerType

func init() {
	var err error
	if Logger, err = utils.NewConsoleLoggerWithName("go-fluentd", "debug"); err != nil {
		utils.Logger.Panic("new logger", zap.Error(err))
	}
}
