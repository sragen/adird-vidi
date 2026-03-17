-- motorcycle.lua — Jakarta Ojek Profile
-- Enables routing through alleys (gang) and footways used by motorcycles

api_version = 4

Set = require('lib/set')
Sequence = require('lib/sequence')
Handlers = require("lib/way_handlers")
Relations = require("lib/relations")
find_access_tag = require("lib/access").find_access_tag
limit = require("lib/maxspeed").limit

function setup()
  return {
    properties = {
      max_speed_for_map_matching      = 110/3.6,
      weight_name                     = 'duration',
      process_call_tagless_node       = false,
      u_turn_penalty                  = 20,
      continue_straight_at_waypoint   = false,
      use_turn_restrictions           = true,
      left_hand_driving               = false,
      traffic_light_penalty           = 2,
    },

    default_mode              = mode.driving,
    default_speed             = 20,
    oneway_handling           = true,
    side_road_multiplier      = 0.8,

    speed_profile = {
      motorway        = 70,
      motorway_link   = 50,
      trunk           = 55,
      trunk_link      = 40,
      primary         = 45,
      primary_link    = 35,
      secondary       = 35,
      secondary_link  = 25,
      tertiary        = 25,
      tertiary_link   = 20,
      unclassified    = 20,
      residential     = 20,
      living_street   = 15,
      service         = 15,
      -- KEY: motorcycle-specific access
      footway         = 10,   -- gang / pedestrian paths
      path            = 10,   -- jalan tikus
      track           = 12,
      steps           = 5,
    },

    -- Motorcycle can access footways and service roads
    access_tag_whitelist = Set {
      'yes',
      'permissive',
      'designated',
      'motorcycle',
      'vehicle',
    },

    access_tag_blacklist = Set {
      'no',
      'private',
    },

    -- Toll roads (motorcycles banned from Jakarta toll roads)
    avoid = Set {
      'toll',
    },

    restricted_access_tag_list = Set {
      'private',
      'delivery',
    },

    access_tags_hierarchy = Sequence {
      'motorcar',
      'motor_vehicle',
      'vehicle',
      'access',
    },

    service_access_tag_blacklist = Set {},

    restrictions = Sequence {
      'motorcar',
      'motor_vehicle',
      'vehicle',
    },
  }
end

function process_node(profile, node, result)
  local access = find_access_tag(node, profile.access_tags_hierarchy)
  if access then
    if profile.access_tag_blacklist[access] then
      result.barrier = true
    end
  end

  local traffic_signal = node:get_value_by_key("highway")
  if traffic_signal == "traffic_signals" then
    result.traffic_lights = true
  end
end

function process_way(profile, way, result)
  local data = {
    highway = way:get_value_by_key('highway'),
    access  = find_access_tag(way, profile.access_tags_hierarchy),
    oneway  = way:get_value_by_key('oneway'),
    junction = way:get_value_by_key('junction'),
  }

  if not data.highway then
    return
  end

  -- Blacklisted access
  if data.access and profile.access_tag_blacklist[data.access] then
    return
  end

  -- Get speed
  local speed = profile.speed_profile[data.highway]
  if not speed then
    return  -- unknown road type, skip
  end

  result.forward_speed = speed
  result.backward_speed = speed
  result.forward_mode = mode.driving
  result.backward_mode = mode.driving

  -- One-way handling
  if data.oneway == "yes" or data.oneway == "1" or data.oneway == "true" then
    result.backward_mode = mode.inaccessible
  elseif data.oneway == "-1" then
    result.forward_mode = mode.inaccessible
  end

  -- Roundabout = one-way
  if data.junction == "roundabout" or data.junction == "circular" then
    result.backward_mode = mode.inaccessible
    result.roundabout = true
  end

  result.name = way:get_value_by_key('name')
end

function process_turn(profile, turn)
  turn.duration = 0
  if turn.is_u_turn then
    turn.duration = turn.duration + profile.properties.u_turn_penalty
  end
  if turn.has_traffic_light then
    turn.duration = turn.duration + profile.properties.traffic_light_penalty
  end
end

return {
  setup = setup,
  process_way = process_way,
  process_node = process_node,
  process_turn = process_turn,
}
