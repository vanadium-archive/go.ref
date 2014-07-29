// TODO: Use minimongo?

var app = app || {};

(function() {
  'use strict';

  var CHANGE = 'change';

  var BaseEvent = function(type) {
    this.type = type;
  };

  var ChangeEvent = function() {
    BaseEvent.bind(this)(CHANGE);
  };

  app.Collection = function(name) {
    this.name_ = name;
    this.vals_ = [];

    this.listeners_ = {};
    this.listeners_[CHANGE] = [];
  };

  app.Collection.prototype = {
    find: function(q, opts) {
      var that = this;
      q = this.normalize_(q);
      var res = _.filter(this.vals_, function(v) {
        return that.matches_(v, q);
      });
      if (opts.sort) {
        // TODO: Eliminate simplifying assumptions.
        var keys = _.keys(opts.sort);
        console.assert(keys.length === 1);
        var key = keys[0];
        console.assert(opts.sort[key] === 1);
        res = res.sort(function(a, b) {
          // TODO: Verify and enhance comparator.
          return a[key] > b[key];
        });
      }
      return _.cloneDeep(res);
    },
    findOne: function(q, opts) {
      var all = this.find(q, opts);
      if (all.length > 0) {
        return all[0];
      }
      return null;
    },
    insert: function(v) {
      console.assert(!_.has(v, '_id'));
      v = _.extend(v, {_id: this.vals_.length});
      this.vals_.push(v);
      this.dispatchEvent_(new ChangeEvent());
      return v._id;
    },
    remove: function(q) {
      var that = this;
      q = this.normalize_(q);
      this.vals_ = _.filter(this.vals_, function(v) {
        return !that.matches_(v, q);
      });
      this.dispatchEvent_(new ChangeEvent());
    },
    update: function(q, opts) {
      var that = this;
      q = this.normalize_(q);
      var vals = _.filter(this.vals_, function(v) {
        return that.matches_(v, q);
      });

      // TODO: Eliminate simplifying assumptions.
      var keys = _.keys(opts);
      console.assert(keys.length === 1);
      var key = keys[0];
      console.assert(_.contains(['$addToSet', '$pull', '$set'], key));
      var opt = opts[key];
      var fields = _.keys(opt);
      console.assert(keys.length === 1);
      var field = fields[0];

      _.each(vals, function(val) {
        switch (key) {
        case '$addToSet':
          val[field] = _.union(val[field], [opt[field]]);
          break;
        case '$pull':
          val[field] = _.without(val[field], opt[field]);
          break;
        case '$set':
          val[field] = opt[field];
          break;
        }
      });

      this.dispatchEvent_(new ChangeEvent());
    },
    addEventListener: function(type, handler) {
      this.listeners_[type].push(handler);
    },
    removeEventListener: function(type, handler) {
      this.listeners_[type] = _.without(this.listeners_[type], handler);
    },
    onChange: function(handler) {
      this.addEventListener(CHANGE, handler);
    },
    normalize_: function(q) {
      if (_.isObject(q)) {
        return q;
      }
      return {_id: q};
    },
    matches_: function(v, q) {
      var keys = _.keys(q);
      for (var i = 0; i < keys.length; i++) {
        var key = keys[i];
        if (_.isArray(v[key]) && !_.isArray(q[key])) {
          if (!_.contains(v[key], q[key])) {
            return false;
          }
        } else {
          if (q[key] !== v[key]) {
            return false;
          }
        }
      }
      return true;
    },
    dispatchEvent_: function(e) {
      _.each(this.listeners_[e.type], function(handler) {
        handler(e);
      });
    }
  };
}());
