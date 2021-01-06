package vfilter

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/Velocidex/ordereddict"
)

// Destructors are stored in the root of the scope stack so they may
// be reached from any nested scope and only destroyed when the root
// scope is destroyed.
type _destructors struct {
	fn           []func()
	is_destroyed bool
	wg           sync.WaitGroup
}

/* The scope is a common environment passed to all plugins, functions
   and operators.

   The scope contains all the client specific code which velocifilter
   will use to actually execute the query. For example, clients may
   add new plugins (See PluginGeneratorInterface{}), functions (see
   FunctionInterface{}) or various protocol implementations to the
   scope prior to evaluating any queries. This is the main mechanism
   where clients may extend and specialize the VQL language.

   The scope also contains convenience functions allowing clients to
   execute available protocols.

   The scope may be populated with free variables that can be
   referenced by the query.
*/
type Scope struct {
	sync.Mutex

	vars      []Row
	functions map[string]FunctionInterface
	plugins   map[string]PluginGeneratorInterface

	bool        _BoolDispatcher
	eq          _EqDispatcher
	lt          _LtDispatcher
	add         _AddDispatcher
	sub         _SubDispatcher
	mul         _MulDispatcher
	div         _DivDispatcher
	membership  _MembershipDispatcher
	associative _AssociativeDispatcher
	regex       _RegexDispatcher
	iterator    _IterateDispatcher

	Logger *log.Logger

	// Very verbose debugging goes here - not generally useful
	// unless users try to debug VQL expressions.
	Tracer *log.Logger

	context *ordereddict.Dict

	stack_depth int

	// All children of this scope.
	children []*Scope

	// Any destructors attached to this scope.
	destructors _destructors
}

// Create a new scope from this scope.
func (self *Scope) NewScope() *Scope {
	self.Lock()
	defer self.Unlock()

	// Make a copy of self
	result := &Scope{
		context: ordereddict.NewDict(),
		vars: []Row{
			ordereddict.NewDict().
				Set("NULL", Null{}),
		},
		functions:   self.functions,
		plugins:     self.plugins,
		bool:        self.bool.Copy(),
		eq:          self.eq.Copy(),
		lt:          self.lt.Copy(),
		add:         self.add.Copy(),
		sub:         self.sub.Copy(),
		mul:         self.mul.Copy(),
		div:         self.div.Copy(),
		membership:  self.membership.Copy(),
		associative: self.associative.Copy(),
		regex:       self.regex.Copy(),
		iterator:    self.iterator.Copy(),
		Logger:      self.Logger,
		Tracer:      self.Tracer,
	}

	return result
}

func (self *Scope) GetContext(name string) Any {
	self.Lock()
	defer self.Unlock()

	res, pres := self.context.Get(name)
	if !pres {
		return nil
	}

	return res
}

func (self *Scope) ClearContext() {
	self.Lock()
	defer self.Unlock()

	self.context = ordereddict.NewDict()
	self.vars = append(self.vars, ordereddict.NewDict().
		Set("NULL", Null{}))
}

func (self *Scope) SetContext(name string, value Any) {
	self.Lock()
	defer self.Unlock()
	self.context.Set(name, value)
}

func (self *Scope) PrintVars() string {
	self.Lock()
	defer self.Unlock()

	my_vars := []string{}
	for _, vars := range self.vars {
		keys := []string{}
		for _, k := range self.GetMembers(vars) {
			keys = append(keys, k)
		}

		my_vars = append(my_vars, "["+strings.Join(keys, ", ")+"]")
	}
	return fmt.Sprintf("Current Scope is: %s", strings.Join(my_vars, ", "))
}

func (self *Scope) Keys() []string {
	self.Lock()
	defer self.Unlock()

	result := []string{}

	for _, vars := range self.vars {
		for _, k := range self.GetMembers(vars) {
			if !InString(&result, k) {
				result = append(result, k)
			}
		}
	}

	return result
}

func (self *Scope) Describe(type_map *TypeMap) *ScopeInformation {
	self.Lock()
	defer self.Unlock()

	result := &ScopeInformation{}
	for _, item := range self.plugins {
		result.Plugins = append(result.Plugins, item.Info(self, type_map))
	}

	for _, func_item := range self.functions {
		result.Functions = append(result.Functions, func_item.Info(self, type_map))
	}

	return result
}

// Tests two values for equality.
func (self *Scope) Eq(a Any, b Any) bool {
	return self.eq.Eq(self, a, b)
}

// Evaluate the truth value of a value.
func (self *Scope) Bool(a Any) bool {
	return self.bool.Bool(self, a)
}

// Is a less than b?
func (self *Scope) Lt(a Any, b Any) bool {
	return self.lt.Lt(self, a, b)
}

// Add a and b together.
func (self *Scope) Add(a Any, b Any) Any {
	return self.add.Add(self, a, b)
}

// Subtract b from a.
func (self *Scope) Sub(a Any, b Any) Any {
	return self.sub.Sub(self, a, b)
}

// Multiply a and b.
func (self *Scope) Mul(a Any, b Any) Any {
	return self.mul.Mul(self, a, b)
}

// Divide b into a.
func (self *Scope) Div(a Any, b Any) Any {
	return self.div.Div(self, a, b)
}

// Is a a member in b?
func (self *Scope) Membership(a Any, b Any) bool {
	return self.membership.Membership(self, a, b)
}

// Get the field member b from a (i.e. a.b).
func (self *Scope) Associative(a Any, b Any) (Any, bool) {
	res, pres := self.associative.Associative(self, a, b)
	return res, pres
}

func (self *Scope) GetMembers(a Any) []string {
	return self.associative.GetMembers(self, a)
}

// Does the regex a match object b.
func (self *Scope) Match(a Any, b Any) bool {
	return self.regex.Match(self, a, b)
}

func (self *Scope) Iterate(ctx context.Context, a Any) <-chan Row {
	return self.iterator.Iterate(ctx, self, a)
}

func (self *Scope) incDepth() {
	self.Lock()
	defer self.Unlock()
	self.stack_depth++
}

func (self *Scope) decDepth() {
	self.Lock()
	defer self.Unlock()
	self.stack_depth--
}

func (self *Scope) getDepth() int {
	self.Lock()
	defer self.Unlock()
	return self.stack_depth
}

func (self *Scope) Copy() *Scope {
	self.Lock()
	defer self.Unlock()

	child_scope := &Scope{
		functions: self.functions,
		plugins:   self.plugins,
		Logger:    self.Logger,
		Tracer:    self.Tracer,
		vars:      append([]Row(nil), self.vars...),
		context:   self.context,

		bool:        self.bool.Copy(),
		eq:          self.eq.Copy(),
		lt:          self.lt.Copy(),
		add:         self.add.Copy(),
		sub:         self.sub.Copy(),
		mul:         self.mul.Copy(),
		div:         self.div.Copy(),
		membership:  self.membership.Copy(),
		associative: self.associative.Copy(),
		regex:       self.regex.Copy(),
		iterator:    self.iterator.Copy(),
		stack_depth: self.stack_depth + 1,
	}

	// Remember our children.
	self.children = append(self.children, child_scope)

	return child_scope
}

// Add various protocol implementations into this
// scope. Implementations must be one of the supported protocols or
// this function will panic.
func (self *Scope) AddProtocolImpl(implementations ...Any) *Scope {
	self.Lock()
	defer self.Unlock()

	for _, imp := range implementations {
		switch t := imp.(type) {
		case BoolProtocol:
			self.bool.AddImpl(t)
		case EqProtocol:
			self.eq.AddImpl(t)
		case LtProtocol:
			self.lt.AddImpl(t)
		case AddProtocol:
			self.add.AddImpl(t)
		case SubProtocol:
			self.sub.AddImpl(t)
		case MulProtocol:
			self.mul.AddImpl(t)
		case DivProtocol:
			self.div.AddImpl(t)
		case MembershipProtocol:
			self.membership.AddImpl(t)
		case AssociativeProtocol:
			self.associative.AddImpl(t)
		case RegexProtocol:
			self.regex.AddImpl(t)
		case IterateProtocol:
			self.iterator.AddImpl(t)
		default:
			Debug(t)
			panic("Unsupported interface")
		}
	}

	return self
}

// Append the variables in Row to the scope.
func (self *Scope) AppendVars(row Row) *Scope {
	self.Lock()
	defer self.Unlock()

	result := self

	result.vars = append(result.vars, row)

	return result
}

// Add client function implementations to the scope. Queries using
// this scope can call these functions from within VQL queries.
func (self *Scope) AppendFunctions(functions ...FunctionInterface) *Scope {
	self.Lock()
	defer self.Unlock()

	result := self
	for _, function := range functions {
		info := function.Info(self, nil)
		result.functions[info.Name] = function
	}

	return result
}

// Add plugins (data sources) to the scope. VQL queries may select
// from these newly added plugins.
func (self *Scope) AppendPlugins(plugins ...PluginGeneratorInterface) *Scope {
	self.Lock()
	defer self.Unlock()

	result := self
	for _, plugin := range plugins {
		info := plugin.Info(self, nil)
		result.plugins[info.Name] = plugin
	}

	return result
}

func (self *Scope) Info(type_map *TypeMap, name string) (*PluginInfo, bool) {
	self.Lock()
	defer self.Unlock()

	if plugin, pres := self.plugins[name]; pres {
		return plugin.Info(self, type_map), true
	}

	return nil, false
}

func (self *Scope) Log(format string, a ...interface{}) {
	self.Lock()
	defer self.Unlock()

	if self.Logger != nil {
		msg := fmt.Sprintf(format, a...)
		self.Logger.Print(msg)
	}
}

func (self *Scope) Trace(format string, a ...interface{}) {
	self.Lock()
	defer self.Unlock()

	if self.Tracer != nil {
		msg := fmt.Sprintf(format, a...)
		self.Tracer.Print(msg)
	}
}

// Adding a destructor to the current scope will call it when any
// parent scopes are closed.
func (self *Scope) AddDestructor(fn func()) {
	self.Lock()
	defer self.Unlock()

	// Scope is already destroyed - call the destructor now.
	if self.destructors.is_destroyed {
		fn()
	} else {
		self.destructors.fn = append(self.destructors.fn, fn)
	}
}

// Closing a scope will also close all its children. Note that
// destructors may use the scope so we can not lock it for the
// duration.
func (self *Scope) Close() {
	self.Lock()
	for _, child := range self.children {
		child.Close()
	}

	// Stop new destructors from appearing.
	self.destructors.is_destroyed = true

	// Remove destructors from list so they are not run again.
	ds := append(self.destructors.fn[:0:0], self.destructors.fn...)
	self.destructors.fn = []func(){}

	// Unlock the scope and start running the
	// destructors. Destructors may actually add new destructors
	// to this scope but hopefully the parent scope will be
	// deleted later.
	self.Unlock()

	// Destructors are called in reverse order to their
	// declerations.
	for i := len(ds) - 1; i >= 0; i-- {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*60)
		go func() {
			ds[i]()
			cancel()
		}()

		select {
		// Wait a maximum 60 seconds for the
		// destructor before moving on.
		case <-ctx.Done():
		}
	}
}

// A factory for the default scope. This will add all built in
// protocols for commonly used code. Clients are expected to add their
// own specialized protocols, functions and plugins to specialize
// their scope objects.
func NewScope() *Scope {
	result := Scope{}
	result.functions = make(map[string]FunctionInterface)
	result.plugins = make(map[string]PluginGeneratorInterface)
	result.context = ordereddict.NewDict()
	result.AppendVars(
		ordereddict.NewDict().
			Set("NULL", Null{}))

	// Protocol handlers.
	result.AddProtocolImpl(
		// Most common objects come first to optimise O(n) algorithm.
		_ScopeAssociative{}, _LazyRowAssociative{}, _DictAssociative{},

		_NullAssociative{}, _NullEqProtocol{}, _NullBoolProtocol{},
		_BoolImpl{}, _BoolInt{}, _BoolString{}, _BoolSlice{}, _BoolDict{},
		_NumericLt{}, _StringLt{},
		_StringEq{}, _IntEq{}, _NumericEq{}, _ArrayEq{}, _DictEq{},
		_AddStrings{}, _AddInts{}, _AddFloats{}, _AddSlices{}, _AddSliceAny{}, _AddNull{},
		_StoredQueryAdd{},
		_SubInts{}, _SubFloats{},
		_SubstringMembership{},
		_MulInt{}, _NumericMul{},
		_NumericDiv{},
		_SubstringRegex{}, _ArrayRegex{},
		_StoredQueryAssociative{}, _StoredQueryBool{},

		_SliceIterator{}, _LazyExprIterator{}, _StoredQueryIterator{}, _DictIterator{},
	)

	// Built in functions.
	result.AppendFunctions(
		_DictFunc{},
		_Timestamp{},
		_SubSelectFunction{},
		_SplitFunction{},
		_IfFunction{},
		_GetFunction{},
		_EncodeFunction{},
		_CountFunction{},
		_MinFunction{},
		_MaxFunction{},
		_EnumerateFunction{},
		_GetVersion{},
		LenFunction{},
	)

	result.AppendPlugins(
		_IfPlugin{},
		_FlattenPluginImpl{},
		_ChainPlugin{},
		_ForeachPluginImpl{},
		&GenericListPlugin{
			PluginName: "scope",
			Function: func(scope *Scope, args *ordereddict.Dict) []Row {
				return []Row{scope}
			},
		},
	)

	return &result
}

// Fetch the field from the scope variables.
func (self *Scope) Resolve(field string) (interface{}, bool) {
	self.Lock()
	defer self.Unlock()

	var default_value Any

	// Walk the scope stack in reverse so more recent vars shadow
	// older ones.
	for i := len(self.vars) - 1; i >= 0; i-- {
		subscope := self.vars[i]

		// Allow each subscope to specify a default. In the
		// end if a default was found then return Resolve as
		// present.
		element, pres := self.Associative(subscope, field)
		if pres {
			// Do not allow go nil to be emitted into the
			// query - this leads to various panics and
			// does not interact well with the reflect
			// package. It is better to emit vfilter Null{}
			// objects which do the right thing when
			// participating in protocols.
			if element == nil {
				element = Null{}
			}
			return element, true
		}

		// Default value of inner most scope will prevail.
		if element != nil && default_value == nil {
			default_value = element
		}
	}

	return default_value, default_value != nil
}

// Scope Associative
type _ScopeAssociative struct{}

func (self _ScopeAssociative) Applicable(a Any, b Any) bool {
	_, a_ok := a.(*Scope)
	_, b_ok := to_string(b)
	return a_ok && b_ok
}

func (self _ScopeAssociative) GetMembers(
	scope *Scope, a Any) []string {
	seen := make(map[string]bool)
	var result []string
	a_scope, ok := a.(Scope)
	if ok {
		for _, vars := range scope.vars {
			for _, member := range a_scope.GetMembers(vars) {
				seen[member] = true
			}
		}

		for k, _ := range seen {
			result = append(result, k)
		}
	}
	return result
}

func (self _ScopeAssociative) Associative(
	scope *Scope, a Any, b Any) (Any, bool) {
	b_str, ok := to_string(b)
	if !ok {
		return nil, false
	}

	a_scope, ok := a.(*Scope)
	if !ok {
		return nil, false
	}
	return a_scope.Resolve(b_str)
}
