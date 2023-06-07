package river

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/meilisearch/meilisearch-go"
	"reflect"
	"strings"
	"time"

	"github.com/go-mysql-org/go-mysql/canal"
	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/go-mysql-org/go-mysql/replication"
	"github.com/go-mysql-org/go-mysql/schema"
	"github.com/juju/errors"
	"github.com/siddontang/go-log/log"
)

const (
	// fixme: 检查格式是否符合meilisearch要求
	fieldTypeList = "list"
	// for the mysql int type to meili date type
	// set the [rule.field] created_time = ",date"
	fieldTypeDate = "date"
)

const mysqlDateFormat = "2006-01-02"

type posSaver struct {
	pos   mysql.Position
	force bool
}

type eventHandler struct {
	r *River
}

func (h *eventHandler) OnRotate(header *replication.EventHeader, rotateEvent *replication.RotateEvent) error {
	pos := mysql.Position{
		Name: string(rotateEvent.NextLogName),
		Pos:  uint32(rotateEvent.Position),
	}

	h.r.syncCh <- posSaver{pos, true}

	return h.r.ctx.Err()
}

func (h *eventHandler) OnTableChanged(header *replication.EventHeader, schema string, table string) error {
	err := h.r.updateRule(schema, table)
	if err != nil && err != ErrRuleNotExist {
		return errors.Trace(err)
	}
	return nil
}

func (h *eventHandler) OnDDL(header *replication.EventHeader, nextPos mysql.Position, queryEvent *replication.QueryEvent) error {
	h.r.syncCh <- posSaver{nextPos, true}
	return h.r.ctx.Err()
}

func (h *eventHandler) OnXID(header *replication.EventHeader, nextPos mysql.Position) error {
	h.r.syncCh <- posSaver{nextPos, false}
	return h.r.ctx.Err()
}

func (h *eventHandler) OnRow(e *canal.RowsEvent) error {
	rule, ok := h.r.rules[ruleKey(e.Table.Schema, e.Table.Name)]
	if !ok {
		return nil
	}

	var reqs []*meilisearch.TaskInfo
	var err error
	switch e.Action {
	case canal.InsertAction:
		reqs, err = h.r.makeInsertRequest(rule, e.Rows)
	case canal.DeleteAction:
		reqs, err = h.r.makeDeleteRequest(rule, e.Rows)
	case canal.UpdateAction:
		reqs, err = h.r.makeUpdateRequest(rule, e.Rows)
	default:
		err = errors.Errorf("invalid rows action %s", e.Action)
	}

	if err != nil {
		h.r.cancel()
		return errors.Errorf("make %s ES request err %v, close sync", e.Action, err)
	}

	h.r.syncCh <- reqs

	return h.r.ctx.Err()
}

func (h *eventHandler) OnGTID(header *replication.EventHeader, gtid mysql.GTIDSet) error {
	return nil
}

func (h *eventHandler) OnPosSynced(header *replication.EventHeader, pos mysql.Position, set mysql.GTIDSet, force bool) error {
	return nil
}

func (h *eventHandler) String() string {
	return "MeiliRiverEventHandler"
}

func (r *River) syncLoop() {
	defer r.wg.Done()

	lastSavedTime := time.Now()

	var pos mysql.Position

	for {
		needSavePos := false

		select {
		case v := <-r.syncCh:
			switch v := v.(type) {
			case posSaver:
				now := time.Now()
				if v.force || now.Sub(lastSavedTime) > 3*time.Second {
					lastSavedTime = now
					needSavePos = true
					pos = v.pos
				}
			case []*meilisearch.TaskInfo:
				for _, req := range v {
					task, err := r.client.WaitForTask(req.TaskUID)
					if err != nil {
						// todo: 重试？
						log.Errorf("meilisearch task failed: %v", err)
					} else {
						log.Debugf("meilisearch task success: %v", task.Type)
					}
				}
			}
		case <-r.ctx.Done():
			return
		}

		if needSavePos {
			if err := r.master.Save(pos); err != nil {
				log.Errorf("save sync position %s err %v, close sync", pos, err)
				r.cancel()
				return
			}
		}
	}
}

// for insert and delete
func (r *River) makeRequest(rule *Rule, action string, rows [][]interface{}) ([]*map[string]interface{}, error) {
	reqs := make([]*map[string]interface{}, 0, len(rows))

	for _, values := range rows {
		id, err := r.getDocID(rule, values)
		if err != nil {
			return nil, errors.Trace(err)
		}

		req := make(map[string]interface{})
		req["id"] = id

		if action == canal.DeleteAction {
			meiliDeleteNum.WithLabelValues(rule.Index).Inc()
		} else {
			r.makeInsertReqData(&req, rule, values)
			meiliInsertNum.WithLabelValues(rule.Index).Inc()
		}

		reqs = append(reqs, &req)
	}

	return reqs, nil
}

func (r *River) makeInsertRequest(rule *Rule, rows [][]interface{}) ([]*meilisearch.TaskInfo, error) {
	reqs, err := r.makeRequest(rule, canal.InsertAction, rows)
	if err != nil {
		return nil, err
	}

	resp, err := r.client.Index(rule.Index).AddDocuments(reqs, "id")

	return []*meilisearch.TaskInfo{resp}, err
}

func (r *River) makeDeleteRequest(rule *Rule, rows [][]interface{}) ([]*meilisearch.TaskInfo, error) {
	ids := make([]string, 0, len(rows))

	for _, values := range rows {
		id, err := r.getDocID(rule, values)
		if err != nil {
			return nil, errors.Trace(err)
		}
		ids = append(ids, id)
	}

	resp, err := r.client.Index(rule.Index).DeleteDocuments(ids)

	return []*meilisearch.TaskInfo{resp}, err
}

func (r *River) makeUpdateRequest(rule *Rule, rows [][]interface{}) ([]*meilisearch.TaskInfo, error) {
	if len(rows)%2 != 0 {
		return nil, errors.Errorf("invalid update rows event, must have 2x rows, but %d", len(rows))
	}

	reqs := make([]*meilisearch.TaskInfo, 0, len(rows))

	for i := 0; i < len(rows); i += 2 {
		beforeID, err := r.getDocID(rule, rows[i])
		if err != nil {
			return nil, errors.Trace(err)
		}

		afterID, err := r.getDocID(rule, rows[i+1])

		if err != nil {
			return nil, errors.Trace(err)
		}

		req := make(map[string]interface{})
		req["id"] = afterID

		if beforeID != afterID {
			resp, err := r.client.Index(rule.Index).DeleteDocument(beforeID)
			if err != nil {
				return nil, errors.Trace(err)
			}
			reqs = append(reqs, resp)

			r.makeInsertReqData(&req, rule, rows[i+1])
			resp, err = r.client.Index(rule.Index).AddDocuments([]map[string]interface{}{req}, "id")
			if err != nil {
				return nil, errors.Trace(err)
			}
			reqs = append(reqs, resp)

			meiliDeleteNum.WithLabelValues(rule.Index).Inc()
			meiliInsertNum.WithLabelValues(rule.Index).Inc()
		} else {
			r.makeUpdateReqData(&req, rule, rows[i], rows[i+1])
			resp, err := r.client.Index(rule.Index).UpdateDocuments([]map[string]interface{}{req}, "id")
			if err != nil {
				return nil, errors.Trace(err)
			}
			reqs = append(reqs, resp)

			meiliUpdateNum.WithLabelValues(rule.Index).Inc()
		}
	}

	return reqs, nil
}

func (r *River) makeReqColumnData(col *schema.TableColumn, value interface{}) interface{} {
	switch col.Type {
	case schema.TYPE_ENUM:
		switch value := value.(type) {
		case int64:
			// for binlog, ENUM may be int64, but for dump, enum is string
			eNum := value - 1
			if eNum < 0 || eNum >= int64(len(col.EnumValues)) {
				// we insert invalid enum value before, so return empty
				log.Warnf("invalid binlog enum index %d, for enum %v", eNum, col.EnumValues)
				return ""
			}

			return col.EnumValues[eNum]
		}
	case schema.TYPE_SET:
		switch value := value.(type) {
		case int64:
			// for binlog, SET may be int64, but for dump, SET is string
			bitmask := value
			sets := make([]string, 0, len(col.SetValues))
			for i, s := range col.SetValues {
				if bitmask&int64(1<<uint(i)) > 0 {
					sets = append(sets, s)
				}
			}
			return strings.Join(sets, ",")
		}
	case schema.TYPE_BIT:
		switch value := value.(type) {
		case string:
			// for binlog, BIT is int64, but for dump, BIT is string
			// for dump 0x01 is for 1, \0 is for 0
			if value == "\x01" {
				return int64(1)
			}

			return int64(0)
		}
	case schema.TYPE_STRING:
		switch value := value.(type) {
		case []byte:
			return string(value[:])
		}
	case schema.TYPE_JSON:
		var f interface{}
		var err error
		switch v := value.(type) {
		case string:
			err = json.Unmarshal([]byte(v), &f)
		case []byte:
			err = json.Unmarshal(v, &f)
		}
		if err == nil && f != nil {
			return f
		}
	case schema.TYPE_DATETIME, schema.TYPE_TIMESTAMP:
		switch v := value.(type) {
		case string:
			vt, err := time.ParseInLocation(mysql.TimeFormat, v, time.Local)
			if err != nil || vt.IsZero() { // failed to parse date or zero date
				return nil
			}
			return vt.Format(time.RFC3339)
		}
	case schema.TYPE_DATE:
		switch v := value.(type) {
		case string:
			vt, err := time.Parse(mysqlDateFormat, v)
			if err != nil || vt.IsZero() { // failed to parse date or zero date
				return nil
			}
			return vt.Format(mysqlDateFormat)
		}
	}

	return value
}

func (r *River) getFieldParts(k string, v string) (string, string, string) {
	composedField := strings.Split(v, ",")

	mysqlCol := k
	meiliCol := composedField[0]
	fieldType := ""

	if 0 == len(meiliCol) {
		meiliCol = mysqlCol
	}
	if 2 == len(composedField) {
		fieldType = composedField[1]
	}

	return mysqlCol, meiliCol, fieldType
}

func (r *River) makeInsertReqData(req *map[string]interface{}, rule *Rule, values []interface{}) {
	for i, c := range rule.TableInfo.Columns {
		if !rule.CheckFilter(c.Name) {
			continue
		}
		mapped := false
		for k, v := range rule.FieldMapping {
			mysqlColName, meiliColName, fieldType := r.getFieldParts(k, v)
			if mysqlColName == c.Name {
				mapped = true
				(*req)[meiliColName] = r.getFieldValue(&c, fieldType, values[i])
			}
		}
		if mapped == false {
			(*req)[c.Name] = r.makeReqColumnData(&c, values[i])
		}
	}
}

func (r *River) makeUpdateReqData(req *map[string]interface{}, rule *Rule,
	beforeValues []interface{}, afterValues []interface{}) {

	for i, c := range rule.TableInfo.Columns {
		mapped := false
		if !rule.CheckFilter(c.Name) {
			continue
		}
		if reflect.DeepEqual(beforeValues[i], afterValues[i]) {
			//nothing changed
			continue
		}
		for k, v := range rule.FieldMapping {
			mysqlColName, meiliColName, fieldType := r.getFieldParts(k, v)
			if mysqlColName == c.Name {
				mapped = true
				(*req)[meiliColName] = r.getFieldValue(&c, fieldType, afterValues[i])
			}
		}
		if mapped == false {
			(*req)[c.Name] = r.makeReqColumnData(&c, afterValues[i])
		}
	}
}

// If id in toml file is none, get primary keys in one row and format them into a string, and PK must not be nil
// Else get the ID's column in one row and format them into a string
func (r *River) getDocID(rule *Rule, row []interface{}) (string, error) {
	var (
		ids []interface{}
		err error
	)
	if rule.ID == nil {
		ids, err = rule.TableInfo.GetPKValues(row)
		if err != nil {
			return "", err
		}
	} else {
		ids = make([]interface{}, 0, len(rule.ID))
		for _, column := range rule.ID {
			value, err := rule.TableInfo.GetColumnValue(column, row)
			if err != nil {
				return "", err
			}
			ids = append(ids, value)
		}
	}

	var buf bytes.Buffer

	sep := ""
	for i, value := range ids {
		if value == nil {
			return "", errors.Errorf("The %ds id or PK value is nil", i)
		}

		buf.WriteString(fmt.Sprintf("%s%v", sep, value))
		sep = ":"
	}

	return buf.String(), nil
}

// get mysql field value and convert it to specific value to meili
func (r *River) getFieldValue(col *schema.TableColumn, fieldType string, value interface{}) interface{} {
	var fieldValue interface{}
	switch fieldType {
	case fieldTypeList:
		v := r.makeReqColumnData(col, value)
		if str, ok := v.(string); ok {
			fieldValue = strings.Split(str, ",")
		} else {
			fieldValue = v
		}

	case fieldTypeDate:
		if col.Type == schema.TYPE_NUMBER {
			col.Type = schema.TYPE_DATETIME

			v := reflect.ValueOf(value)
			switch v.Kind() {
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				fieldValue = r.makeReqColumnData(col, time.Unix(v.Int(), 0).Format(mysql.TimeFormat))
			}
		}
	}

	if fieldValue == nil {
		fieldValue = r.makeReqColumnData(col, value)
	}
	return fieldValue
}
