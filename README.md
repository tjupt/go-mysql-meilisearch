go-mysql-meilisearch is a service syncing your MySQL data into meilisearch automatically.

It uses `mysqldump` to fetch the origin data at first, then syncs data incrementally with binlog.

## Install

+ `go get github.com/tjupt/go-mysql-meilisearch`, it will print some messages in console, skip it. :-)
+ cd `$GOPATH/src/github.com/tjupt/go-mysql-meilisearch`
+ `make`

## How to use?

+ Create table in MySQL.
+ Config base, see the example config [river.toml](./etc/river.toml).
+ Set MySQL source in config file, see [Source](#source) below.
+ Customize MySQL and meilisearch mapping rule in config file, see [Rule](#rule) below.
+ Start `./bin/go-mysql-meilisearch -config=./etc/river.toml` and enjoy it.

## Notice

+ binlog format must be **row**.
+ binlog row image must be **full** for MySQL, you may lost some field data if you update PK data in MySQL with minimal or noblob binlog row image. MariaDB only supports full row image.
+ Can not alter table format at runtime.
+ MySQL table which will be synced should have a PK(primary key), multi columns PK is allowed now, e,g, if the PKs is (a, b), we will use "a:b" as the key. The PK data will be used as "id" in meilisearch. And you can also config the id's constituent part with other column.
+ You should create the associated mappings in meilisearch first, I don't think using the default mapping is a wise decision, you must know how to search accurately.
+ `mysqldump` must exist in the same node with go-mysql-meilisearch, if not, go-mysql-meilisearch will try to sync binlog only.
+ Don't change too many rows at same time in one SQL.

## Source

In go-mysql-meilisearch, you must decide which tables you want to sync into meilisearch in the source config.

The format in config file is below:

```
[[source]]
schema = "test"
tables = ["t1", t2]

[[source]]
schema = "test_1"
tables = ["t3", t4]
```

`schema` is the database name, and `tables` includes the table need to be synced.

If you want to sync **all table in database**, you can use **asterisk(\*)**.  
```
[[source]]
schema = "test"
tables = ["*"]

# When using an asterisk, it is not allowed to sync multiple tables
# tables = ["*", "table"]
```

## Rule

By default, go-mysql-meilisearch will use MySQL table name as the meilisearch's index and type name, use MySQL table field name as the meilisearch's field name.  
e.g, if a table named blog, the default index and type in meilisearch are both named blog, if the table field named title,
the default field name is also named title.

Rule can let you change this name mapping. Rule format in config file is below:

```
[[rule]]
schema = "test"
table = "t1"
index = "t"
id = ["id"]

    [rule.field]
    mysql = "title"
    meili = "my_title"
```

In the example above, we will use a new index and type both named "t" instead of default "t1", and use "my_title" instead of field name "title".

## Rule field types

In order to map a mysql column on different meilisearch types you can define the field type as follows:

```
[[rule]]
schema = "test"
table = "t1"
index = "t"

    [rule.field]
    // This will map column title to meilisearch my_title
    title="my_title"

    // This will map column title to meilisearch my_title and use array type
    title="my_title,list"

    // This will map column title to meilisearch title and use array type
    title=",list"

    // If the created_time field type is "int", and you want to convert it to "date" type in es, you can do it as below
    created_time=",date"
```

Modifier "list" will translates a mysql string field like "a,b,c" on a meilisearch array type '{"a", "b", "c"}' this is specially useful if you need to use those fields on filtering on meilisearch.

## Wildcard table

go-mysql-meilisearch only allows you determine which table to be synced, but sometimes, if you split a big table into multi sub tables, like 1024, table_0000, table_0001, ... table_1023, it is very hard to write rules for every table.

go-mysql-meilisearch supports using wildcard table, e.g:

```
[[source]]
schema = "test"
tables = ["test_river_[0-9]{4}"]

[[rule]]
schema = "test"
table = "test_river_[0-9]{4}"
index = "river"
```

"test_river_[0-9]{4}" is a wildcard table definition, which represents "test_river_0000" to "test_river_9999", at the same time, the table in the rule must be same as it.

At the above example, if you have 1024 sub tables, all tables will be synced into meilisearch with index "river" and type "river".

## Filter fields

You can use `filter` to sync specified fields, like:

```
[[rule]]
schema = "test"
table = "tfilter"
index = "test"

# Only sync following columns
filter = ["id", "name"]
```

In the above example, we will only sync MySQL table tfilter's columns `id` and `name` to meilisearch. 

## Ignore table without a primary key
When you sync table without a primary key, you can see below error message.
```
schema.table must have a PK for a column
```
You can ignore these tables in the configuration like:
```
# Ignore table without a primary key
skip_no_pk_table = true
```
