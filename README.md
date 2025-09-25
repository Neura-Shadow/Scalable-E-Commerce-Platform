# 🛒 Go E-Commerce Platform

[![Master](https://github.com/quangdangfit/goshop/workflows/master/badge.svg)](https://github.com/quangdangfit/goshop/actions)
[![Codecov](https://codecov.io/gh/quangdangfit/goshop/graph/badge.svg?token=78BO8FQDB0)](https://codecov.io/gh/quangdangfit/goshop)
![Go Version](https://img.shields.io/github/go-mod/go-version/quangdangfit/goshop?style=flat-square)
[![License](https://img.shields.io/github/license/jrapoport/gothic?style=flat-square)](https://github.com/quangdangfit/goshop/blob/master/LICENSE)

Go Shop is an **open-source e-commerce backend template** written in Go with [Gin](https://github.com/gin-gonic/gin).  
It demonstrates a production-ready architecture with **REST, gRPC, DDD, authentication, database, caching, Swagger docs**, and more.

---

## 🚀 Features
- RESTful API with Gin  
- gRPC support  
- Domain-Driven Design (DDD) structure  
- PostgreSQL with GORM ORM  
- Redis cache integration  
- JWT-based authentication  
- Centralized logging  
- Auto-generated API docs with Swagger  

---

## 📋 Requirements
- [Go](https://golang.org/) **1.17+**  
- [Docker](https://docs.docker.com/get-docker/)  
- [Docker Compose](https://docs.docker.com/compose/)  
- [PostgreSQL](https://www.postgresql.org/)  
- [Redis](https://redis.io/)  

👉 You can use the [docker-compose template](https://github.com/quangdangfit/docker-compose-template/blob/master/base/docker-compose.yml) to set up PostgreSQL and Redis quickly.

---

## ⚙️ Setup & Run (All In One)

```bash
# 1. Clone the repository
git clone https://github.com/quangdangfit/goshop.git
cd goshop

# 2. Copy and edit config
cp pkg/config/config.sample.yaml pkg/config/config.yaml

# 3. Example config (pkg/config/config.yaml)
environment: production
http_port: 8888
grpc_port: 8889
auth_secret: your-secret-key
database_uri: postgres://username:password@host:5432/database
redis_uri: localhost:6379
redis_password:
redis_db: 0

# 4. Run the server
go run cmd/api/main.go
