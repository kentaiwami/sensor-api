# sensor-api 開発ログ

## 概要

温度・湿度・CO2センサーの値をPOSTで受け取りDBに登録するGo製REST API。

---

## API仕様

| エンドポイント | メソッド | 説明 |
|---|---|---|
| `/health` | GET | ヘルスチェック |
| `/temperature` | POST | 温度を記録 |
| `/humidity` | POST | 湿度を記録 |
| `/co2` | POST | CO2濃度を記録 |

### 認証

全エンドポイント（`/health`除く）に`X-API-Key`ヘッダーが必要。

### リクエストボディ

```json
{
  "value": 25.3,
  "sensor_id": "living-room"
}
```

- `value`: 必須
- `sensor_id`: 必須（センサーの識別子）

### レスポンス

| ステータス | 説明 |
|---|---|
| 201 | 登録成功 |
| 400 | リクエスト不正 |
| 401 | 認証失敗 |
| 500 | サーバーエラー |

---

## DB設計

MariaDB。テーブルは3つ、構造は共通。

```sql
CREATE TABLE IF NOT EXISTS temperatures (
    id          BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    sensor_id   VARCHAR(64)  NOT NULL,
    value       DECIMAL(5,2) NOT NULL,
    recorded_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP
);
-- humidities, co2s も同構造
```

### タイムゾーン設定

`/etc/my.cnf.d/mariadb-server.cnf` の `[mysqld]` セクションに追加：

```ini
default-time-zone = '+09:00'
```

---

## 環境変数

| 変数名 | 説明 |
|---|---|
| `DATABASE_URL` | MariaDB接続文字列 |
| `API_KEY` | APIキー |

`.env`ファイルで管理（`godotenv`で読み込み）。OSの環境変数が優先される。

---

## ローカル開発

```bash
docker compose up --build
```

MariaDB + APIが起動する。`.env`を用意しておくこと。

```bash
# 動作確認
curl -X POST http://localhost:8080/temperature \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-api-key" \
  -d '{"value": 25.3, "sensor_id": "living-room"}'
```

---

## インフラ構成

```
Internet
  → ALB (HTTP:80)
    → NodePort(32224)
      → k3s Pod × 2 (node1, node2)
        → MariaDB (registry+dbサーバー, port 3306)
```

### k3s Secret

```bash
kubectl create secret generic sensor-api-secret \
  --from-literal=database-url='user:pass@tcp(host:3306)/sensordb?parseTime=true' \
  --from-literal=api-key='ランダム文字列' \
  -n sensor-api
```

APIキーの生成：
```bash
openssl rand -hex 32
```

---

## CI/CD（GitHub Actions）

mainブランチへのpushで自動デプロイ。

**Self-hosted Runner**: k3s control-planeに配置。

```bash
# Runnerのサービス登録
sudo ./svc.sh install
sudo ./svc.sh start
```

**GitHub Secrets:**

| キー | 値 |
|---|---|
| `REGISTRY_HOST` | RegistryサーバーのプライベートIP |

### フロー

1. `docker build` & Registryへpush（コミットハッシュでタグ付け）
2. `manifests/deployment.yaml`のプレースホルダーを置換して`kubectl apply`
3. `kubectl rollout status`でデプロイ完了を確認

---

## CodePipelineを採用しなかった理由

CodeBuildからk3sへのデプロイにSSH(22)が必要で、CodeBuildのIPが毎回変わるため実質`0.0.0.0/0`で開ける必要がある。VPC内CodeBuildにすればSGで絞れるがNATゲートウェイが必要でコスト増。

GitHub ActionsのSelf-hosted RunnerはアウトバウンドのみでGitHubに接続するため、インバウンドポートを一切開けなくて良い。個人開発ではGitHub Actionsが最適。

---

## 今後やること

- サブドメインでのルーティング（Route 53 + ACM + ALBホストベースルーティング）
  - `api.kentaiwami.com` → sensor-api
  - 他サービスも同じALBで振り分け
