package vfilter

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"www.velocidex.com/golang/vfilter/marshal"
	"www.velocidex.com/golang/vfilter/types"
)

type StoredQueryItem struct {
	Query      string   `json:"query,omitempty"`
	Name       string   `json:"name,omitempty"`
	Parameters []string `json:"parameters,omitempty"`
}

func (self *_StoredQuery) Marshal(
	scope types.Scope) (*types.MarshalItem, error) {

	var query string
	if self.parameters == nil {
		query = fmt.Sprintf("LET `%v` = %s", self.name, self.query.ToString(scope))
	} else {
		query = fmt.Sprintf("LET `%v`(%s) = %s", self.name,
			strings.Join(self.parameters, ", "),
			self.query.ToString(scope))
	}

	query_str, err := json.Marshal(query)
	return &types.MarshalItem{
		Type: "Replay",
		Data: query_str,
	}, err
}

func (self *StoredExpression) Marshal(
	scope types.Scope) (*types.MarshalItem, error) {

	var query string
	if self.parameters == nil {
		query = fmt.Sprintf("LET `%v` = %s", self.name,
			self.Expr.ToString(scope))
	} else {
		query = fmt.Sprintf("LET `%v`(%s) = %s", self.name,
			strings.Join(self.parameters, ", "),
			self.Expr.ToString(scope))
	}

	query_str, err := json.Marshal(query)
	return &types.MarshalItem{
		Type: "Replay",
		Data: query_str,
	}, err
}

type ReplayUnmarshaller struct{}

func (self ReplayUnmarshaller) Unmarshal(
	unmarshaller types.Unmarshaller,
	scope types.Scope, item *types.MarshalItem) (interface{}, error) {
	var query string
	err := json.Unmarshal(item.Data, &query)
	if err != nil {
		return nil, err
	}

	vql, err := Parse(query)
	if err != nil {
		return nil, err
	}

	for _ = range vql.Eval(context.Background(), scope) {
	}

	// Return nil here indicates not to set the value into the
	// scope (since we already did in the Replay above).
	return nil, nil
}

func NewUnmarshaller(ignore_vars []string) *marshal.Unmarshaller {
	unmarshaller := marshal.NewUnmarshaller()
	unmarshaller.Handlers["Scope"] = ScopeUnmarshaller{ignore_vars}
	unmarshaller.Handlers["Replay"] = ReplayUnmarshaller{}

	return unmarshaller
}