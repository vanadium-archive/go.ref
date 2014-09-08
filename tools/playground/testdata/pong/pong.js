var veyron = require('veyron');

var pingPongService = {
  ping: function(msg){
    console.log(msg);
    return 'PONG';
  }
};

veyron.init(function(err, rt) {
  if (err) throw err;

  rt.serve('pingpong', pingPongService, function(err) {
    if (err) throw err;
  });
});
