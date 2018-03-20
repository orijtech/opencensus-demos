## media-search

An app that uses BadgerDB as the key-value store for lightning speed.
However this version of BadgerDB is instrumented with OpenCensus and
since it has context propagation, clients can send their traces
end to end, receive and export them to their choices of backends.

### Requirements
* Instrumented BadgerDB:
Get it at and install it normally https://github.com/orijtech/badger/tree/opencensus-instrumented

* Python3 and Go:
The server is in Go, and is accessed by API clients in various languages
such as Python3, Go, Java etc. 

The Python client requires the Python OpenCensus package to be installed.
The Go server requires you to run `go get -u ./media-search`

Giving such:

##### Go server
```shell
GOOGLE_APPLICATION_CREDENTIALS=~/Downloads/census-demos-237a8e1e41df.json go run main.go 

2018/03/20 12:44:30 Finished exporter registration
2018/03/20 12:44:30 Serving on ":9778"
2018/03/20 12:45:35 item: key="World News", version=2, meta=42
2018/03/20 12:45:46 item: key="Elementary", version=3, meta=42
2018/03/20 12:49:53 item: key="Elementary", version=3, meta=42
2018/03/20 13:00:07 item: key="SQL", version=6, meta=42
```

##### Python client

```shell
python3 search.py
Content to search$ SQL
URL: https://youtu.be/7Vtl2WggqOg
Title: SQL for Beginners. Learn basics  of SQL in 1 Hour
Description: SQL is a special-purpose programming language designed for managing data in a relational database, and is used by a huge number of apps and organizations. Buy SQL Books from Amazon at http://amzn.t...


URL: https://youtu.be/nWeW3sCmD2k
Title: SQL Crash Course - Beginner to Intermediate
Description: In this course we will cover all of the fundamentals of the SQL (Structured Query Language). This course is great for beginners and also intermediates. We will cover the following... Overview...


URL: https://youtu.be/FR4QIeZaPeM
Title: What is Database & SQL?
Description: https://www.guru99.com/introduction-to-database-sql.html This Database tutorial explains the concept of DBMS (Database Management System), examples of real life DBMS systems and types of...


Title: SQL Server tutorial for beginners
Description: In this tutorial, we will start from the very basics and cover topics like joins, views, triggers, system functions, stored procedures, user defined scalar and table valued functions etc. These...


URL: https://youtu.be/9Pzj7Aj25lw
Title: Learn SQL in 1 Hour - SQL Basics for Beginners
Description: A crash course in SQL. How to write SQL from scratch in 1 hour. In this video I show you how to write SQL using SQL Server and SQL Server Management Studio. We go through Creating a Database,...


{'traceId': '67b37388177d4db4a1f99683f3bf4bac', 'spans': [{'displayName': {'value': 'py-search', 'truncated_byte_count': 0}, 'spanId': 8159224170534666, 'startTime': '2018-03-20T20:00:07.740696Z', 'endTime': '2018-03-20T20:00:07.756807Z', 'childSpanCount': 0}]}
```

### Screenshots
The clients' HTTP requests propagate their
traces through to the server and back, and then to the exporters yielding
insights such as:

#### HTTP requests
![](./images/stackdriver-http-request.png)
![](./images/x-ray-http-request.png)

#### DB operations
![](./images/stackdriver-badgerdb.png)
![](./images/x-ray-badgerdb.png)
