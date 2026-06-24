#!/usr/bin/env bash
# Интеграционный тест драфта: 20 игроков, 4 капитана, полный проход пиков.
set -e
BASE=http://localhost:8080
TMP=/tmp/draft_test
mkdir -p $TMP
SUF=$RANDOM   # уникальный суффикс, чтобы не конфликтовать с прошлыми прогонами

declare -A IDBY    # hex id -> индекс пользователя
declare -A JARBY   # hex id -> файл cookie

echo "=== Регистрация 20 игроков ==="
for i in $(seq 1 20); do
  if [ $i -le 4 ]; then CAT="Captain"; else CAT=$(echo "A B C" | tr ' ' '\n' | sed -n "$(( (i % 3) + 1 ))p"); fi
  JAR=$TMP/cj_$i.txt
  RESP=$(curl -s -c $JAR -X POST $BASE/auth/register -H "Content-Type: application/json" \
    -d "{\"phone\":\"+7700${SUF}00$i\",\"email\":\"u${SUF}_$i@t.su\",\"password\":\"secret1\",\"password_confirm\":\"secret1\",\"nickname\":\"u${SUF}_$i\",\"real_name\":\"U$i\",\"category\":\"$CAT\"}")
  ID=$(echo "$RESP" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
  IDBY[$ID]=$i
  JARBY[$ID]=$JAR
  eval "ID_$i=$ID"
done
echo "ok"

CREATOR_JAR=$TMP/cj_1.txt
echo "=== Создание лобби (creator=u1) ==="
LID=$(curl -s -b $CREATOR_JAR -X POST $BASE/lobby/create -H "Content-Type: application/json" \
  -d '{"name":"DraftTest","type":"dvacatka"}' | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
echo "lobby=$LID"

echo "=== Вход остальных 19 игроков ==="
for i in $(seq 2 20); do
  curl -s -b $TMP/cj_$i.txt -X POST $BASE/lobby/$LID/join > /dev/null
done
echo "players: $(curl -s -b $CREATOR_JAR $BASE/lobby/$LID | grep -o '"players_count":[0-9]*')"

echo "=== Старт драфта ==="
curl -s -b $CREATOR_JAR -X POST $BASE/lobby/$LID/start-draft > /dev/null
echo "status: $(curl -s -b $CREATOR_JAR $BASE/lobby/$LID | grep -o '"status":"[^"]*"' | head -1)"

echo "=== Капитаны занимают слоты (u1->t1 ... u4->t4) ==="
for t in 1 2 3 4; do
  curl -s -b $TMP/cj_$t.txt -X POST $BASE/lobby/$LID/claim-captain/$t > /dev/null
done
PICKING=$(curl -s -b $CREATOR_JAR $BASE/lobby/$LID/draft-state | grep -o '"picking":[a-z]*')
echo "picking=$PICKING"
echo "order: $(curl -s -b $CREATOR_JAR $BASE/lobby/$LID/draft-state | grep -o '"order":\[[^]]*\]')"

echo "=== Проверка сортировки доступных игроков (категории должны идти A→B→C→Captain) ==="
curl -s -b $CREATOR_JAR $BASE/lobby/$LID/draft-state | python -c "
import sys,json
cats=[p['category'] for p in json.load(sys.stdin)['available']]
rank={'A':0,'B':1,'C':2,'Captain':3}
ranks=[rank.get(c,4) for c in cats]
print('категории:', cats)
print('ОТСОРТИРОВАНО ПО КАТЕГОРИИ:', ranks==sorted(ranks))
"

echo "=== Тест отмены пика ==="
ST=$(curl -s -b $CREATOR_JAR $BASE/lobby/$LID/draft-state)
read CAP0 CT0 AV0 <<< $(echo "$ST" | python -c "import sys,json;d=json.load(sys.stdin);print(d['current_captain'],d['current_team'],d['available'][0]['user_id'])")
AVAIL_BEFORE=$(echo "$ST" | python -c "import sys,json;print(len(json.load(sys.stdin)['available']))")
echo "до пика: ход у команды $CT0, доступно $AVAIL_BEFORE"
curl -s -b ${JARBY[$CAP0]} -X POST $BASE/lobby/$LID/pick/$AV0 > /dev/null
ST=$(curl -s -b $CREATOR_JAR $BASE/lobby/$LID/draft-state)
AVAIL_AFTER=$(echo "$ST" | python -c "import sys,json;print(len(json.load(sys.stdin)['available']))")
CT1=$(echo "$ST" | python -c "import sys,json;print(json.load(sys.stdin)['current_team'])")
echo "после пика: ход у команды $CT1, доступно $AVAIL_AFTER (должно стать на 1 меньше)"
curl -s -b $CREATOR_JAR -X POST $BASE/lobby/$LID/undo-pick > /dev/null
ST=$(curl -s -b $CREATOR_JAR $BASE/lobby/$LID/draft-state)
AVAIL_UNDO=$(echo "$ST" | python -c "import sys,json;print(len(json.load(sys.stdin)['available']))")
CT2=$(echo "$ST" | python -c "import sys,json;print(json.load(sys.stdin)['current_team'])")
echo "после ОТМЕНЫ: ход у команды $CT2, доступно $AVAIL_UNDO"
echo "ОТМЕНА КОРРЕКТНА: $([ "$AVAIL_UNDO" = "$AVAIL_BEFORE" ] && [ "$CT2" = "$CT0" ] && echo ДА || echo НЕТ)"

echo "=== Пики по очереди (16 игроков) ==="
for n in $(seq 1 16); do
  ST=$(curl -s -b $CREATOR_JAR $BASE/lobby/$LID/draft-state)
  # Достаём current_captain, current_team и первого доступного игрока через python.
  read CAP CT AVAIL <<< $(echo "$ST" | python -c "import sys,json; d=json.load(sys.stdin); print(d['current_captain'], d['current_team'], d['available'][0]['user_id'])")
  JAR=${JARBY[$CAP]}
  RES=$(curl -s -b $JAR -X POST $BASE/lobby/$LID/pick/$AVAIL)
  OK=$(echo "$RES" | grep -o '"complete":[a-z]*')
  echo "pick $n: team=$CT captain=u${IDBY[$CAP]} -> u${IDBY[$AVAIL]}  ($OK)"
done

echo "=== Итоговые команды ==="
curl -s -b $CREATOR_JAR $BASE/lobby/$LID/draft-state | python -c "import sys,json; d=json.load(sys.stdin); [print(t['name'],'cap=',t['captain_id'][-4:],'slots=',len(t['slots'])) for t in d['teams']]; print('COMPLETE:', d['complete'])" 2>/dev/null || curl -s -b $CREATOR_JAR $BASE/lobby/$LID/draft-state | grep -o '"complete":[a-z]*'

echo "=== Генерация сетки ==="
curl -s -b $CREATOR_JAR -X POST $BASE/lobby/$LID/generate-bracket > /dev/null
echo "status: $(curl -s -b $CREATOR_JAR $BASE/lobby/$LID | grep -o '"status":"[^"]*"' | head -1)"
curl -s -b $CREATOR_JAR $BASE/lobby/$LID | python -c "
import sys,json
d=json.load(sys.stdin); b=d['bracket']
for ri,r in enumerate(b['rounds']):
  print('Round',ri+1,':', [(m['id'], m.get('team1'),'vs',m.get('team2'),'W='+str(m.get('winner'))) for m in r['matches']])
"

echo "=== Ввод результатов до победителя ==="
for step in 1 2 3 4 5; do
  ST=$(curl -s -b $CREATOR_JAR $BASE/lobby/$LID)
  # Находим первый матч, где обе команды есть и нет победителя.
  MID=$(echo "$ST" | python -c "
import sys,json
d=json.load(sys.stdin)
for r in d['bracket']['rounds']:
  for m in r['matches']:
    if m.get('team1') is not None and m.get('team2') is not None and m.get('winner') is None:
      print(m['id']); sys.exit()
print('NONE')")
  if [ "$MID" = "NONE" ]; then echo "нет матчей к вводу — сетка сыграна"; break; fi
  curl -s -b $CREATOR_JAR -X POST $BASE/lobby/$LID/match/$MID/result -H "Content-Type: application/json" -d '{"score1":16,"score2":12}' > /dev/null
  echo "введён результат матча id=$MID (16:12)"
done

echo "=== Финальная сетка ==="
curl -s -b $CREATOR_JAR $BASE/lobby/$LID | python -c "
import sys,json
d=json.load(sys.stdin); b=d['bracket']
for ri,r in enumerate(b['rounds']):
  print('Round',ri+1,':', [(m['id'], m.get('team1'),'vs',m.get('team2'),'=>W'+str(m.get('winner')), str(m['score1'])+':'+str(m['score2'])) for m in r['matches']])
"

echo "=== Объявление победителя ==="
curl -s -b $CREATOR_JAR -X POST $BASE/lobby/$LID/finish | python -c "import sys,json; d=json.load(sys.stdin); print('status=',d['status'],'winner_team_id=',d['winner_team_id'])"

echo "=== Проверка истории игр у игрока u1 ==="
curl -s -b $CREATOR_JAR $BASE/profile | python -c "import sys,json; d=json.load(sys.stdin); print('game_history count=', len(d['game_history']))"

# cleanup
echo "$LID" > /tmp/draft_test_lid.txt
