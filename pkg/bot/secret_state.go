package bot

// Secret state contains IDs, deadlines and fixed result text only. Small
// ordering tombstones persist to reject replay after session expiry. Uncertain locks require
// an administrator's review; they must never expire into an automatic retry.
const redisSecretScript = `
local op, user, chat, update, message, result, uncertain = unpack(ARGV)
local now=tonumber(redis.call('TIME')[1])
local function done()
  redis.call('SET',KEYS[2],cjson.encode({user=user,chat=chat,state='done',result=result,completed_at=now,uncertain=false}), 'EX',604800)
end
local function response(state, text)
  return cjson.encode({state=state,result=text or ''})
end
local event = redis.call('GET',KEYS[2])
if event then
  local e=cjson.decode(event)
  if e.user~=user or e.chat~=chat then return redis.error_reply('event owner mismatch') end
  if op~='finish' or e.state~='working' then return response(e.state,e.result) end
elseif op=='finish' then
  return redis.error_reply('event missing')
end
if op=='peek' then
  local session=redis.call('GET',KEYS[1])
  if session and cjson.decode(session).expires>tonumber(redis.call('TIME')[1]) then return response('waiting') end
  return response('missing')
end
if op=='finish' then
  if redis.call('GET',KEYS[3])~=update then return redis.error_reply('lock owner mismatch') end
  if uncertain=='false' then
    done()
  else
    redis.call('SET',KEYS[2],cjson.encode({user=user,chat=chat,state='done',result=result,completed_at=now,uncertain=true}))
  end
  if uncertain=='false' then redis.call('DEL',KEYS[3]) end
  return response('done',result)
end
if redis.call('EXISTS',KEYS[3])==1 then return response('locked') end
local raw=redis.call('GET',KEYS[1])
local s=nil
if raw then s=cjson.decode(raw) end
-- Compare decimal IDs as strings to avoid Lua's 53-bit floating point limit.
local function newer(a,b)
  if #a~=#b then return #a>#b end
  return a>b
end
-- Keep only IDs indefinitely, so expiring sessions cannot reset ordering.
local order=redis.call('GET',KEYS[4])
local previous=order and cjson.decode(order) or s
if s and (not previous or newer(s.update,previous.update)) then previous=s end
local function saveorder()
  redis.call('SET',KEYS[4],cjson.encode({update=update,message=message}))
end
if op=='begin' then
  if previous and (not newer(update,previous.update) or not newer(message,previous.message)) then return response('expired') end
  saveorder()
  redis.call('SET',KEYS[1],cjson.encode({update=update,message=message,expires=now+300}), 'EX',300)
elseif op=='claim' or op=='invalid' then
  if not s or s.expires<=now or not newer(update,s.update) or not newer(message,s.message) then return response('expired') end
  saveorder()
  s.update=update
  s.message=message
  if op=='claim' then
    redis.call('DEL',KEYS[1])
    redis.call('SET',KEYS[3],update)
    redis.call('SET',KEYS[2],cjson.encode({user=user,chat=chat,state='working',result=''}))
    return response('claimed')
  end
  redis.call('SET',KEYS[1],cjson.encode(s),'EX',s.expires-now)
else
  return redis.error_reply('invalid operation')
end
done()
return response('done',result)
`
