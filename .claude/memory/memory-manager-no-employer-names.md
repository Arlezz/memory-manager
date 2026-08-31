---
name: memory-manager-no-employer-names
description: el repo memory-manager no debe nombrar al empleador ni a clientes; vocabulario de fixtures genericos ORBIT-X/ACME-DEV/gitlab.example.com
metadata: 
  node_type: memory
  type: project
  originSessionId: 4a902265-6735-48e3-8379-d9bfd8bb2e36
  modified: 2026-08-30T21:28:44.743Z
---

Regla fijada por Anton el **2026-08-30** para [[memory-manager-goal]]: el repo publico `github.com/Arlezz/memory-manager` **no puede nombrar a su empleador ni a ningun cliente o proyecto del trabajo**. Es un proyecto personal y el codigo se publica en npm y como plugin.

Alcanza a los fixtures de test y a los ejemplos de la documentacion. Los originales se habian copiado tal cual del survey de remotes de la maquina de Anton; se reemplazaron en el arbol y en los 4 commits (reescritos con `filter-branch` el 2026-08-30, antes del primer push).

**Esta memoria no lista los nombres reales a proposito.** Escribirlos aca reconstruiria el indice que la limpieza elimino.

## Vocabulario de fixtures — usar estos para cualquier caso nuevo

- Organizaciones: `ACME-DEV`, `ORBIT-DEV`, `contoso`
- Repos: `ORBIT-X_core`, `ORBIT-X_billing`, `ORBIT-X_data_pipeline`, `route_optimizer`, `atlas`, `MVP-DEMO`, `admin-frontend`, `vendor_experiments`, `poc-control-room-3`
- Hosts: `gitlab.example.com`
- Segmentos de ruta: `projects`, `internal`
- Tokens falsos: prefijo `GITLAB-FAKE`

Los fixtures conservan **la forma** que los tests ejercitan — mayusculas, guion medio, guion bajo, doble guion bajo, grupo anidado, puerto SSH no default, host en mayusculas — asi que `Normalize`/`Slugify` siguen cubriendo los mismos casos. Un fixture nuevo debe imitar la forma, nunca el nombre.

Se conservan a proposito: `Anton`, `anton`, `antony`, `Arlezz` — identidad propia del dueno del repo.

## Lo que la limpieza NO alcanza

El **runtime** si registra los repos reales, y es inevitable con identidad = remote normalizado (ver [[memory-manager-tech-decisions]]): `~/.claude/memory-manager/state/<slug>.json` guarda slug, canonical y rutas absolutas, y el repo personal privado tiene un `projects/<slug>/` por cada repo. O sea: el repo personal privado es, por construccion, el listado de los repos en los que Anton trabaja. Es privado, pero conviene tenerlo explicito. Salida si molesta: hashear el slug (`sha256[:16]`) y dejar el canonical solo dentro del JSON — cambio acotado a `Slugify`, a costa de que `status` y el arbol dejen de ser legibles.

Los transcripts `.jsonl` de las sesiones tampoco se limpian con nada de esto.

**Why:** un identificador de cliente en un repo publico es una filtracion, y los tests de `internal/identity` estaban llenos de remotes reales del trabajo.

**How to apply:** antes de cualquier push, grepear el arbol y `git rev-list refs/heads/main` por los nombres del empleador y de los clientes. Nunca inventar un fixture nuevo con un nombre real: tomar uno de la lista de arriba.
