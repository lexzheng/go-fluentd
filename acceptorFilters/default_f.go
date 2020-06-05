package acceptorFilters

import (
	"github.com/Laisky/go-fluentd/libs"
	"github.com/Laisky/zap"
)

type DefaultFilterCfg struct {
	RemoveEmptyTag, RemoveUnsupportTag bool
	AcceptTags                         []string
	Name                               string
	libs.AddCfg
}

type DefaultFilter struct {
	*BaseFilter
	*DefaultFilterCfg
	tags map[string]struct{}
}

func NewDefaultFilter(cfg *DefaultFilterCfg) *DefaultFilter {
	f := &DefaultFilter{
		BaseFilter:       &BaseFilter{},
		DefaultFilterCfg: cfg,
		tags:             map[string]struct{}{},
	}

	for _, tag := range cfg.AcceptTags {
		f.tags[tag] = struct{}{}
	}

	libs.Logger.Info("new default filter",
		zap.Strings("accept_tags", f.AcceptTags),
		zap.Bool("remove_empty_tag", f.RemoveEmptyTag),
		zap.Bool("remove_unknown_tag", f.RemoveUnsupportTag),
	)
	return f
}

func (f *DefaultFilter) GetName() string {
	return f.Name
}

func (f *DefaultFilter) isTagAccepted(tag string) (ok bool) {
	_, ok = f.tags[tag]
	return ok
}

func (f *DefaultFilter) Filter(msg *libs.FluentMsg) *libs.FluentMsg {
	if f.RemoveEmptyTag && msg.Tag == "" {
		libs.Logger.Warn("discard log since empty tag", zap.String("tag", msg.Tag))
		f.DiscardMsg(msg)
		return nil
	}

	if f.RemoveUnsupportTag && !f.isTagAccepted(msg.Tag) {
		libs.Logger.Warn("discard log since unsupported tag", zap.String("tag", msg.Tag))
		f.DiscardMsg(msg)
		return nil
	}

	libs.ProcessAdd(f.AddCfg, msg)
	return msg
}
