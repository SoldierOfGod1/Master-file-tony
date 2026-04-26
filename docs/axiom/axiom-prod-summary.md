# Axiom Prod Catalogue

Cluster: `axiom-prod-pg-cluster.rain.co.za:5433`  
Discovered: `2026-04-19T19:36:41Z`  
Databases crawled: **26** of 46 total  
Columns indexed: **8,361**

## Top-level map

| DB | Size | Schemas | Tables crawled |
|---|---|---|---|
| `communication` | 1680.4 GB | 11 | 113 |
| `resource` | 801.0 GB | 5 | 121 |
| `customer` | 656.3 GB | 3 | 80 |
| `payment` | 294.7 GB | 4 | 91 |
| `account` | 222.9 GB | 3 | 73 |
| `product` | 213.2 GB | 2 | 60 |
| `digital` | 104.7 GB | 5 | 72 |
| `snowflake` | 98.1 GB | 6 | 35 |
| `logistics` | 51.3 GB | 9 | 99 |
| `prepay` | 30.1 GB | 2 | 38 |
| `shopping` | 26.6 GB | 2 | 36 |
| `party` | 21.3 GB | 3 | 57 |
| `service` | 21.2 GB | 3 | 66 |
| `rica` | 2.3 GB | 2 | 12 |
| `risk` | 1005 MB | 2 | 17 |
| `raingo` | 524 MB | 2 | 10 |
| `porting` | 297 MB | 2 | 6 |
| `geographic` | 177 MB | 2 | 3 |
| `raindrop` | 34 MB | 8 | 49 |
| `prompt` | 11 MB | 2 | 14 |
| `stock` | 11 MB | 2 | 34 |
| `document` | 9 MB | 2 | 26 |
| `trouble` | 8 MB | 1 | 3 |
| `promotion` | 8 MB | 1 | 3 |
| `quote` | 8 MB | 1 | 3 |
| `entity` | 8 MB | 1 | 4 |

## Heaviest tables across the cluster (by row estimate)

| Rows | Table |
|---:|---|
| 433,923,520 | `account.account.billing_account_history` |
| 212,962,128 | `communication.notification.sms_history` |
| 198,462,224 | `communication.chat.chat_history` |
| 108,190,800 | `service.public.service_history_unnest` |
| 101,246,264 | `customer.public.invoice_items` |
| 100,407,648 | `product.public.product_price_backup_fdw_mv` |
| 95,567,760 | `account.account.account_balance_history` |
| 85,777,112 | `communication.notification.rain_inbox` |
| 82,782,768 | `customer.customer_bill.applied_customer_billing_rate_2026` |
| 78,488,016 | `snowflake.public.agent_action_history` |
| 76,496,056 | `account.public.tmp_sms` |
| 71,197,408 | `resource.ralf.connection_log` |
| 70,564,240 | `customer.customer_bill.financial_statement` |
| 67,581,296 | `payment.payment.payment` |
| 66,575,268 | `snowflake.public.note` |
| 65,996,500 | `customer.public.tmp_customer_bill_all` |
| 65,216,796 | `customer.public.invoices` |
| 58,584,204 | `customer.customer_bill.financial_statement_old` |
| 55,748,964 | `payment.payment.peach_transaction` |
| 53,657,940 | `snowflake.public.action_ticket_variables` |
| 52,145,828 | `account.public.payment_20260218` |
| 50,712,768 | `snowflake.public.state_change` |
| 48,926,504 | `digital.digital.credential` |
| 47,619,696 | `payment.public.tmp_payment` |
| 44,515,712 | `resource.ralf.request` |

## Likely cross-DB join keys (by column-name frequency)

| column | occurrences | databases present |
|---|---:|---|
| `id` | 532 | 20 |
| `billing_account_id` | 72 | 10 |
| `financial_account_id` | 60 | 6 |
| `related_party_id` | 59 | 11 |
| `msisdn` | 52 | 8 |
| `email` | 50 | 11 |
| `customer_id` | 40 | 5 |
| `product_id` | 38 | 8 |
| `user_id` | 36 | 12 |
| `campaign_id` | 31 | 4 |
| `service_id` | 28 | 9 |
| `batch_id` | 26 | 4 |
| `applied_tax_id` | 22 | 1 |
| `channel_id` | 21 | 5 |
| `order_id` | 21 | 5 |
| `bill_cycle_id` | 20 | 3 |
| `payment_id` | 20 | 5 |
| `payment_method_id` | 19 | 4 |
| `notification_id` | 19 | 1 |
| `tenant_id` | 19 | 1 |
| `header_id` | 18 | 1 |
| `trace_id` | 18 | 1 |
| `connection_log_id` | 18 | 1 |
| `connection_id` | 18 | 1 |
| `bill_structure_id` | 17 | 5 |
| `provider_channel_id` | 16 | 1 |
| `bill_id` | 14 | 1 |
| `external_id` | 14 | 7 |
| `registration_id` | 13 | 2 |
| `account_id` | 12 | 5 |