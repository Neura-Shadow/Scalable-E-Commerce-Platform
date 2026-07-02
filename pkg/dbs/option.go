package dbs

import "gorm.io/gorm/clause"

type FindOption interface {
	apply(*option)
}

type option struct {
	query    []Query
	order    any
	offset   int
	limit    int
	preloads []string
}

type optionFn func(*option)

func (f optionFn) apply(opt *option) {
	f(opt)
}

func WithQuery(query ...Query) FindOption {
	return optionFn(func(opt *option) {
		opt.query = query
	})
}

func WithOffset(offset int) FindOption {
	return optionFn(func(opt *option) {
		opt.offset = offset
	})
}

func WithLimit(limit int) FindOption {
	return optionFn(func(opt *option) {
		opt.limit = limit
	})
}

func WithOrder(order clause.OrderByColumn) FindOption {
	return optionFn(func(opt *option) {
		opt.order = order
	})
}

func SafeOrderByColumn(orderBy string, desc bool, defaultColumn string, allowedColumns map[string]string) clause.OrderByColumn {
	column := defaultColumn
	useDesc := false
	if allowedColumn, ok := allowedColumns[orderBy]; orderBy != "" && ok {
		column = allowedColumn
		useDesc = desc
	}

	return clause.OrderByColumn{
		Column: clause.Column{Name: column},
		Desc:   useDesc,
	}
}

func WithPreload(preloads []string) FindOption {
	return optionFn(func(opt *option) {
		opt.preloads = preloads
	})
}

func getOption(opts ...FindOption) option {
	opt := option{
		query:  []Query{},
		offset: 0,
		limit:  1000,
		order:  clause.OrderByColumn{Column: clause.Column{Name: "id"}},
	}

	for _, o := range opts {
		o.apply(&opt)
	}

	return opt
}
