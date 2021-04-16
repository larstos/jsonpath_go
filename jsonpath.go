package jsonpath_go

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/larstos/convert"
)

var emptyStep = step{}

//LookupRaw support search by jpath, input as json byte and search path
func LookupRaw(jsbyte []byte, jpath string) (interface{}, error) {
	var json_data interface{}
	err := json.Unmarshal(jsbyte, &json_data)
	if err != nil {
		return nil, err
	}
	return Lookup(json_data, jpath)
}

//ReplaceRaw support replace by jpath, input as json byte and replace target value by search path
func ReplaceRaw(jsbyte []byte, jpath string, object interface{}) (interface{}, error) {
	var json_data interface{}
	err := json.Unmarshal(jsbyte, &json_data)
	if err != nil {
		return nil, err
	}
	err = Replace(json_data, jpath, object)
	if err != nil {
		return nil, err
	}
	return json_data, nil
}

//Lookup support search by jpath
//Attention:object must be filled with []interface or map[string]interface{}
func Lookup(obj interface{}, jpath string) (interface{}, error) {
	c, err := Compile(jpath)
	if err != nil {
		return nil, err
	}
	return c.Lookup(obj)
}

//Replace support search by jpath, and replace them with replaceObj.
// Attention object must be filled with []interface or map[string]interface{}.
func Replace(obj interface{}, jpath string, replaceObj interface{}) error {
	c, err := Compile(jpath)
	if err != nil {
		return err
	}
	return c.Replace(obj, replaceObj)
}

type Compiled struct {
	path  string
	steps []step
}

func Compile(jpath string) (*Compiled, error) {
	tokens, err := tokenize(jpath)
	if err != nil {
		return nil, err
	}
	if tokens[0] != "@" && tokens[0] != "$" {
		return nil, fmt.Errorf("$ or @ should in front of path")
	}
	tokens = tokens[1:]
	res := Compiled{
		path:  jpath,
		steps: make([]step, len(tokens)),
	}
	for i, token := range tokens {
		temstep, err := parse_token(token)
		if err != nil {
			return nil, err
		}
		res.steps[i] = temstep
	}
	return &res, nil
}

func MustCompile(jpath string) *Compiled {
	c, err := Compile(jpath)
	if err != nil {
		panic(err)
	}
	return c
}

func (c *Compiled) String() string {
	return fmt.Sprintf("Compiled lookup: %s", c.path)
}

//Lookup support search by jpath.
//Attention:object must be filled with []interface or map[string]interface{}
func (c *Compiled) Lookup(obj interface{}) (interface{}, error) {
	if obj == nil {
		return nil, errors.New("get attribute from null object")
	}
	var err error
	rootnode := obj
	var findnomap bool
	for idx := 0; idx < len(c.steps); idx++ {
		s := c.steps[idx]
		if !findnomap && !s.singleReturn() {
			findnomap = true
		}
		if s.op == "search" {
			s.next = &c.steps[idx+1]
			idx++
		}
		// "key", "idx"
		obj, err = s.parse(obj, rootnode)
		if err != nil {
			return nil, err
		}
		if _, ok := obj.([]interface{}); ok && findnomap && idx != len(c.steps)-1 {
			temc := &Compiled{
				steps: c.steps[idx+1:],
			}
			oarr := obj.([]interface{})
			newret := make([]interface{}, 0, len(oarr))
			for _, i := range oarr {
				switch i.(type) {
				case []interface{}, map[string]interface{}:
					r, err := temc.Lookup(i)
					if err != nil {
						return nil, err
					}
					if r == nil {
						continue
					}
					if rlist, ok := r.([]interface{}); ok {
						newret = append(newret, rlist...)
					} else {
						newret = append(newret, r)
					}
				}
			}
			return newret, nil
		}
	}
	return obj, nil
}

//Replace support replace by jpath with replacement.
//Attention:object must be filled with []interface or map[string]interface{}
func (c *Compiled) Replace(obj, replacement interface{}) error {
	if obj == nil || replacement == nil {
		return errors.New("get attribute from null object")
	}
	if len(c.steps) == 0 {
		return errors.New("empty search content")
	}
	//mark replacement
	c.steps[len(c.steps)-1].replaceVal = replacement
	//do search
	_, err := c.Lookup(obj)
	if err != nil {
		return err
	}
	return nil
}

type step struct {
	op         string
	key        string
	args       interface{}
	filter     filterParam
	replaceVal interface{}
	next       *step
}

func tokenize(query string) ([]string, error) {
	if query[0] != '$' && query[0] != '@' {
		return nil, fmt.Errorf("should start with '$' or '@'")
	}
	if strings.HasSuffix(query, ".") {
		return nil, fmt.Errorf("should end with '.'")
	}
	//make sure follow condition works
	// 1. $[0].enmu => "$","[0],"enmu"
	// 2. $.enmu =>"$",enmu
	startidx := 1
	if query[startidx] == '.' {
		startidx++
	}
	tokens := append([]string{string(query[0])}, strings.Split(query[startidx:], ".")...)
	rets := make([]string, 0, len(tokens))
	sublist := make([]string, 0, len(tokens))
	subleft := 0
	for idx := 0; idx < len(tokens); idx++ {
		token := tokens[idx]
		switch token {
		case "":
			if len(rets) > 0 && rets[len(rets)-1] == " " {
				continue
			}
			sublist = append(sublist, " ")
		default:
			//findout regexp
			if strings.Index(token, "=~") > 0 {
				subleft = subleft + strings.Count(token[:idx], "[")
				endidx := strings.Index(token, "/i")
				for endidx < 0 && idx < len(tokens)-1 {
					sublist = append(sublist, token)
					idx++
					token = tokens[idx]
					endidx = strings.Index(token, "/i")
				}
				if idx == len(tokens)-1 && endidx < 0 {
					return nil, errors.New("regex has no end")
				}
				subleft = subleft - strings.Count(token[endidx:], "]")
			} else {
				for _, tv := range token {
					switch tv {
					case '[':
						subleft++
					case ']':
						subleft--
					}
				}
			}
			sublist = append(sublist, token)
		}
		if subleft == 0 {
			rets = append(rets, strings.Join(sublist, "."))
			sublist = make([]string, 0, len(tokens)-idx)
		}
	}
	if subleft > 0 {
		return nil, errors.New("invalid query find [ without ]")
	}
	return rets, nil
}

func parse_token(token string) (step, error) {
	if token == "$" || token == "@" {
		return step{
			op:   "root",
			key:  "$",
			args: nil,
		}, nil
	}
	if token == "*" {
		return step{
			op:   "scan",
			key:  "*",
			args: nil,
		}, nil
	}
	if token == " " {
		return step{
			op:   "search",
			key:  "",
			args: nil,
		}, nil
	}

	bracket_idx := strings.Index(token, "[")
	if bracket_idx < 0 {
		return step{
			op:   "key",
			key:  token,
			args: nil,
		}, nil
	}
	key := token[:bracket_idx]
	tail := token[bracket_idx:]
	if len(tail) < 3 {
		return step{}, fmt.Errorf("len(tail) should >=3, %v", tail)
	}
	tail = tail[1 : len(tail)-1]
	var retstep = step{
		key: key,
	}
	if strings.Contains(tail, "?") {
		retstep.op = "filter"
		if strings.HasPrefix(tail, "?(") && strings.HasSuffix(tail, ")") {
			filterpart := strings.TrimSpace(tail[2 : len(tail)-1])
			retstep.args = filterpart
			tfilter, err := parse_filter(filterpart)
			if err != nil {
				return retstep, err
			}
			retstep.filter = tfilter
		}
	} else if strings.Contains(tail, ":") {
		// range ----------------------------------------------
		retstep.op = "range"
		tails := strings.Split(tail, ":")
		if len(tails) != 2 {
			return retstep, fmt.Errorf("only support one range(from, to): %v", tails)
		}
		var frm interface{}
		var to interface{}
		var err error
		tails[0] = strings.TrimSpace(tails[0])
		if tails[0] != "" {
			if frm, err = strconv.Atoi(strings.TrimSpace(tails[0])); err != nil {
				return step{}, fmt.Errorf("range index not int: %v", tails)
			}
		}
		tails[1] = strings.TrimSpace(tails[1])
		if tails[1] != "" {
			if to, err = strconv.Atoi(strings.TrimSpace(tails[1])); err != nil {
				return step{}, fmt.Errorf("range index not int: %v", tails)
			}
		}
		retstep.args = [2]interface{}{frm, to}
	} else if tail == "*" {
		retstep.op = "range"
		retstep.args = [2]interface{}{nil, nil}
	} else {
		// idx ------------------------------------------------
		retstep.op = "idx"
		res := []int{}
		for _, x := range strings.Split(tail, ",") {
			if i, err := strconv.Atoi(strings.TrimSpace(x)); err == nil {
				res = append(res, i)
			} else {
				return step{}, err
			}
		}
		retstep.args = res
	}
	return retstep, nil
}

func (s step) singleReturn() bool {
	if s.op == "idx" && len(s.args.([]int)) == 1 {
		return true
	}
	return s.op == "key"
}

func (s step) parse(obj interface{}, rootnode interface{}) (interface{}, error) {
	var err error
	switch s.op {
	case "key":
		obj, err = s.get_key(obj, s.key)
		if err != nil {
			return nil, err
		}
	case "idx":
		if len(s.key) > 0 {
			obj, err = emptyStep.get_key(obj, s.key)
			if err != nil {
				return nil, err
			}
		}
		intarg, ok := s.args.([]int)
		if !ok {
			return nil, errors.New("range config not number index")
		}
		if len(intarg) > 1 {
			res := make([]interface{}, 0, len(intarg))
			for _, x := range intarg {
				tmp, err := s.get_idx(obj, x)
				if err != nil {
					return nil, err
				}
				if tmp != nil {
					res = append(res, tmp)
				}
			}
			obj = res
		} else if len(intarg) == 1 {
			obj, err = s.get_idx(obj, intarg[0])
			if err != nil {
				return nil, err
			}
		} else {
			return nil, fmt.Errorf("cannot index on empty slice")
		}
	case "range":
		if len(s.key) > 0 {
			// no key `$[:1].test`
			obj, err = emptyStep.get_key(obj, s.key)
			if err != nil {
				return nil, err
			}
		}
		if argsv, ok := s.args.([2]interface{}); ok == true {
			obj, err = s.get_range(obj, argsv[0], argsv[1])
			if err != nil {
				return nil, err
			}
		} else {
			return nil, fmt.Errorf("range args length should be 2")
		}
	case "filter":
		if s.key != "" {
			obj, err = emptyStep.get_key(obj, s.key)
			if err != nil {
				return nil, err
			}
		}
		obj, err = s.get_filtered(obj, rootnode, s.args.(string))
		if err != nil {
			return nil, err
		}
	case "scan":
		obj, err = s.get_scan(obj)
		if err != nil {
			return nil, err
		}
	case "root":
		return obj, err
	case "search":
		return s.get_search(obj, rootnode)
	default:
		return nil, fmt.Errorf("expression don't support in filter %s", s.op)
	}
	return obj, nil
}

func (s step) get_key(obj interface{}, key string) (interface{}, error) {
	if obj == nil {
		return nil, nil
	}
	switch reflect.TypeOf(obj).Kind() {
	case reflect.Map:
		if jsonMap, ok := obj.(map[string]interface{}); ok {
			val, exists := jsonMap[key]
			if !exists {
				return nil, nil
			}
			if s.replaceVal != nil {
				jsonMap[key] = s.replaceVal
			}
			return val, nil
		}
		return nil, fmt.Errorf("key error: %s not found in object", key)
	default:
		return nil, nil
	}
}

func (s step) get_idx(obj interface{}, idx int) (interface{}, error) {
	jsonarr, ok := obj.([]interface{})
	if !ok {
		return nil, nil
	}
	length := len(jsonarr)
	if idx >= 0 {
		if idx >= length {
			return nil, fmt.Errorf("index out of range: len: %v, idx: %v", length, idx)
		}
		if s.replaceVal != nil {
			jsonarr[idx] = s.replaceVal
		}
		return jsonarr[idx], nil
	}
	// < 0
	_idx := length + idx
	if _idx < 0 {
		return nil, fmt.Errorf("index out of range: len: %v, idx: %v", length, idx)
	}
	if s.replaceVal != nil {
		jsonarr[_idx] = s.replaceVal
	}
	return jsonarr[_idx], nil
}

func (s step) get_range(obj, frm, to interface{}) ([]interface{}, error) {
	jsonarr, ok := obj.([]interface{})
	if !ok {
		return nil, fmt.Errorf("object is not Slice")
	}
	length := len(jsonarr)
	_frm := 0
	_to := length
	if frm == nil {
		frm = 0
	}
	if to == nil {
		to = length - 1
	}
	if fv, ok := frm.(int); ok == true {
		if fv < 0 {
			_frm = length + fv
		} else {
			_frm = fv
		}
	}
	if tv, ok := to.(int); ok == true {
		if tv < 0 {
			_to = length + tv + 1
		} else {
			_to = tv + 1
		}
	}
	if _frm < 0 || _frm >= length {
		return nil, fmt.Errorf("index [from] out of range: len: %v, from: %v", length, frm)
	}
	if _to < 0 || _to > length {
		return nil, fmt.Errorf("index [to] out of range: len: %v, to: %v", length, to)
	}
	if s.replaceVal != nil {
		for i := _frm; i < _to; i++ {
			jsonarr[i] = s.replaceVal
		}
	}
	return jsonarr[_frm:_to], nil
}

func (s step) get_scan(obj interface{}) ([]interface{}, error) {
	if obj == nil {
		return nil, nil
	}
	switch reflect.TypeOf(obj).Kind() {
	case reflect.Map:
		if jsonMap, ok := obj.(map[string]interface{}); ok {
			retval := make([]interface{}, 0, len(jsonMap))
			for s2, v := range jsonMap {
				if v == nil {
					continue
				}
				if s.replaceVal != nil {
					jsonMap[s2] = s.replaceVal
				}
				retval = append(retval, jsonMap[s2])
			}
			return retval, nil
		}
		return []interface{}{}, nil
	case reflect.Slice:
		// slice we should get from all objects in it.
		if jsonarr, ok := obj.([]interface{}); ok {
			retval := make([]interface{}, 0, len(jsonarr))
			for s2, v := range jsonarr {
				if v == nil {
					continue
				}
				if s.replaceVal != nil {
					jsonarr[s2] = s.replaceVal
				}
				retval = append(retval, jsonarr[s2])
			}
			return retval, nil
		}
		return []interface{}{}, nil
	default:
		return nil, nil
	}
}

func (s step) get_filtered(obj, root interface{}, filter string) ([]interface{}, error) {
	var res []interface{}
	switch jobject := obj.(type) {
	case []interface{}:
		for i, tmp := range jobject {
			ok, err := s.filter.eval_filter(tmp, root)
			if err != nil {
				return nil, err
			}
			if ok == true {
				if s.replaceVal != nil {
					jobject[i] = s.replaceVal
				}
				res = append(res, tmp)
			}
		}
		return res, nil
	case map[string]interface{}:
		for k, tmp := range jobject {
			ok, err := s.filter.eval_filter(tmp, root)
			if err != nil {
				return nil, err
			}
			if ok == true {
				if s.replaceVal != nil {
					jobject[k] = s.replaceVal
				}
				res = append(res, tmp)
			}
		}
	default:
		return nil, nil
	}

	return res, nil
}

func (s step) get_search(obj interface{}, root interface{}) ([]interface{}, error) {
	ret := make([]interface{}, 0)
	robj, err := s.next.parse(obj, root)
	if err != nil {
		return nil, err
	}
	if olist, ok := robj.([]interface{}); ok && len(olist) > 0 {
		ret = append(ret, olist...)
	} else if robj != nil {
		ret = append(ret, robj)
	}
	var slist []interface{}
	slist, err = emptyStep.get_scan(obj)
	if err != nil {
		return nil, err
	}
	if len(slist) > 0 {
		for _, i := range slist {
			cret, err := s.get_search(i, root)
			if err != nil {
				return nil, err
			}
			ret = append(ret, cret...)
		}
	}
	return ret, nil
}

type filterParam struct {
	lp          string
	op          string
	rp          string
	reg         *regexp.Regexp
	lpSubSearch *Compiled
	rpSubSearch *Compiled
}

// @.isbn                 => @.isbn, exists, nil
// @.price < 10           => @.price, <, 10
// @.price <= $.expensive => @.price, <=, $.expensive
// @.author =~ /.*REES/i  => @.author, match, /.*REES/i
func parse_filter(filter string) (filterParam, error) {
	filterObj := filterParam{}
	tmp := ""
	stage := 0
	str_embrace := false
	for idx, c := range filter {
		switch c {
		case '\'':
			if str_embrace == false {
				str_embrace = true
			} else {
				switch stage {
				case 0:
					filterObj.lp = tmp
				case 1:
					filterObj.op = tmp
				case 2:
					filterObj.rp = tmp
				}
				tmp = ""
			}
		case ' ':
			if str_embrace == true {
				tmp += string(c)
				continue
			}
			switch stage {
			case 0:
				filterObj.lp = tmp
			case 1:
				filterObj.op = tmp
			case 2:
				filterObj.rp = tmp
			}
			tmp = ""

			stage += 1
			if stage > 2 {
				return filterParam{}, errors.New(fmt.Sprintf("invalid char at %d: `%c`", idx, c))
			}
		default:
			tmp += string(c)
		}
	}
	if tmp != "" {
		switch stage {
		case 0:
			filterObj.lp = tmp
			filterObj.op = "exists"
		case 1:
			filterObj.op = tmp
		case 2:
			filterObj.rp = tmp
		}
		tmp = ""
	}
	err := filterObj.preCompile()
	if err != nil {
		return filterParam{}, err
	}
	return filterObj, nil
}

func (p *filterParam) preCompile() error {
	if p.op == "=~" {
		var err error
		p.reg, err = regFilterCompile(p.rp)
		if err != nil {
			return err
		}
	}
	if strings.HasPrefix(p.lp, "@.") || strings.HasPrefix(p.lp, "$.") {
		c, err := Compile(p.lp)
		if err != nil {
			return err
		}
		p.lpSubSearch = c
	}
	if strings.HasPrefix(p.rp, "@.") || strings.HasPrefix(p.rp, "$.") {
		c, err := Compile(p.rp)
		if err != nil {
			return err
		}
		p.rpSubSearch = c
	}
	return nil
}

func regFilterCompile(rule string) (*regexp.Regexp, error) {
	if len(rule) <= 3 {
		return nil, errors.New("empty rule")
	}
	if !strings.HasPrefix(rule, "/") || !strings.HasSuffix(rule, "/i") {
		return nil, errors.New("invalid syntax. should be in `/pattern/` form")
	}
	return regexp.Compile(strings.TrimSuffix(strings.TrimPrefix(rule, "/"), "/i"))
}

//eval_filter find out if obj suit filter expression.Logic should use this func
func (p filterParam) eval_filter(obj, root interface{}) (bool, error) {
	if p.op == "=~" {
		if p.reg == nil {
			return false, errors.New("regexp not been init")
		}
		return p.eval_reg_filter(obj, root)
	}
	return p.eval_filter_normal(obj, root)
}

//eval_reg_filter  check value with rule of regexp(ex. @.author =~ /.*REES/i )
func (p filterParam) eval_reg_filter(obj, root interface{}) (res bool, err error) {
	lp_v, err := p.get_lp_v(obj, root)
	if err != nil {
		return false, err
	}
	switch v := lp_v.(type) {
	case string:
		return p.reg.MatchString(v), nil
	default:
		return false, errors.New("only string can match with regular expression")
	}
}

//eval_filter_normal  check value with rule of normal compare(ex. @.price < 10 )
func (p filterParam) eval_filter_normal(obj, root interface{}) (res bool, err error) {
	if p.op == "=~" {
		return false, fmt.Errorf("should not be here")
	}
	lp_v, err := p.get_lp_v(obj, root)
	if p.op == "exists" {
		return lp_v != nil, nil
	}
	rp_v, err := p.get_rp_v(obj, root)
	if err != nil {
		return false, err
	}
	return p.cmp_any(lp_v, rp_v)
}

//get_lp_v get compare expression left value
func (p filterParam) get_lp_v(obj, root interface{}) (interface{}, error) {
	var lp_v interface{}
	if strings.HasPrefix(p.lp, "@.") {
		return p.lpSubSearch.Lookup(obj)
	} else if strings.HasPrefix(p.lp, "$.") {
		return p.lpSubSearch.Lookup(root)
	} else {
		lp_v = p.lp
	}
	return lp_v, nil
}

//get_lp_v get compare expression right value
func (p filterParam) get_rp_v(obj, root interface{}) (interface{}, error) {
	var rp_v interface{}
	if strings.HasPrefix(p.rp, "@.") {
		return p.rpSubSearch.Lookup(obj)
	} else if strings.HasPrefix(p.rp, "$.") {
		return p.rpSubSearch.Lookup(root)
	} else {
		rp_v = p.rp
	}
	return rp_v, nil
}

//cmp_any compare normal style
func (p filterParam) cmp_any(obj1, obj2 interface{}) (bool, error) {
	fval1, ok1 := convert.TryFloat64(obj1)
	fval2, ok2 := convert.TryFloat64(obj2)
	isnumber := ok1 && ok2
	switch p.op {
	case "<":
		if isnumber {
			return fval1 < fval2, nil
		}
		return convert.MustString(obj1) < convert.MustString(obj2), nil
	case "<=":
		if isnumber {
			return fval1 <= fval2, nil
		}
		return convert.MustString(obj1) <= convert.MustString(obj2), nil
	case "==":
		if isnumber {
			return fval1 == fval2, nil
		}
		return convert.MustString(obj1) == convert.MustString(obj2), nil
	case ">=":
		if isnumber {
			return fval1 >= fval2, nil
		}
		return convert.MustString(obj1) >= convert.MustString(obj2), nil
	case ">":
		if isnumber {
			return fval1 > fval2, nil
		}
		return convert.MustString(obj1) > convert.MustString(obj2), nil
	default:
		return false, fmt.Errorf("op should only be <, <=, ==, >= and >")
	}
}
