-- All keys share a target hash slot. Classification and changes are atomic.
local raw=redis.call('GET',KEYS[1])
if not raw then return 'missing' end
local ok,s=pcall(cjson.decode,raw)
if not ok or type(s)~='table' then return 'retained' end
if redis.call('EXISTS',KEYS[2])==1 then return 'protected' end
local now=tonumber(redis.call('TIME')[1])
local apply=ARGV[2]=='apply'
local function decimal(v)
  return type(v)=='string' and string.match(v,'^%d+$') and (#v==1 or string.sub(v,1,1)~='0')
end
local function newer(a,b)
  if #a~=#b then return #a>#b end
  return a>b
end
if ARGV[1]=='session' then
  if type(s.expires)~='number' or s.expires~=math.floor(s.expires) or s.expires<0 or s.expires>now+300 or not decimal(s.update) or not decimal(s.message) then return 'retained' end
  local order=redis.call('GET',KEYS[3])
  local previous=nil
  if order then
    local valid,value=pcall(cjson.decode,order)
    if not valid or type(value)~='table' or not decimal(value.update) or not decimal(value.message) then return 'retained' end
    previous=value
  end
  if apply then
    local u,m=s.update,s.message
    if previous then
      if newer(previous.update,u) then u=previous.update end
      if newer(previous.message,m) then m=previous.message end
    end
    redis.call('SET',KEYS[3],cjson.encode({update=u,message=m}))
    if s.expires<=now then redis.call('DEL',KEYS[1])
    else
      local ttl=redis.call('TTL',KEYS[1])
      if ttl<0 or ttl>s.expires-now then redis.call('EXPIREAT',KEYS[1],s.expires) end
    end
  end
  if s.expires<=now then return 'expired-session' end
  return 'session-ttl'
elseif ARGV[1]=='event' then
  -- Legacy events have no reliable timestamp or certainty flag: retain them.
  if s.state~='done' or s.uncertain~=false or type(s.completed_at)~='number' or s.completed_at~=math.floor(s.completed_at) or s.completed_at<=0 or s.completed_at>now then return 'retained' end
  local deadline=s.completed_at+604800
  if apply then
    if deadline<=now then redis.call('DEL',KEYS[1])
    else
      local ttl=redis.call('TTL',KEYS[1])
      if ttl<0 or ttl>deadline-now then redis.call('EXPIREAT',KEYS[1],deadline) end
    end
  end
  if deadline<=now then return 'expired-event' end
  return 'event-ttl'
end
return 'retained'
