(function() {
  const canvas = document.getElementById("bgCanvas");
  if (!canvas) return;
  const ctx = canvas.getContext("2d");
  const NODE_COUNT = 20;

  const palette = [
    { r: 195, g: 86, b: 43 },
    { r: 195, g: 143, b: 119 },
    { r: 174, g: 162, b: 153 },
  ];

  function resize() {
    canvas.width = window.innerWidth;
    canvas.height = window.innerHeight;
  }
  resize();
  window.addEventListener("resize", resize);

  class NodeShape {
    constructor() {
      this.x = Math.random() * canvas.width;
      this.y = Math.random() * canvas.height;
      this.vx = (Math.random() - 0.5) * 0.12;
      this.vy = (Math.random() - 0.5) * 0.12;
      this.baseAngle = Math.random() * Math.PI * 2;
      this.angle = this.baseAngle;
      this.rotationSpeed = (Math.random() - 0.5) * 0.0015;
      this.maxRotation = Math.PI / 6;
      this.size = 50 + Math.random() * 50;
      this.opacity = 0.7 + Math.random() * 0.15;
      this.col = palette[Math.floor(Math.random() * palette.length)];
      this.dotR = 4 + Math.random() * 3;
      this.lineWidth = 1.2 + Math.random() * 0.6;
      this.points = this._genPoints();
    }

    _genPoints() {
      const count = Math.random() < 0.3 ? 3 : 2;
      const pts = [];
      for (let i = 0; i < count; i++) {
        pts.push({ angle: Math.random() * Math.PI * 2, length: this.size });
      }
      return pts;
    }

    update() {
      this.x += this.vx;
      this.y += this.vy;
      if (this.x < 0 || this.x > canvas.width) this.vx *= -1;
      if (this.y < 0 || this.y > canvas.height) this.vy *= -1;
      this.angle += this.rotationSpeed;
      const delta = this.angle - this.baseAngle;
      if (delta > this.maxRotation || delta < -this.maxRotation) {
        this.rotationSpeed *= -1;
      }
    }

    draw(ctx) {
      const c = this.col;
      const stroke = "rgba(" + c.r + "," + c.g + "," + c.b + "," + this.opacity + ")";
      const fill = "rgba(" + c.r + "," + c.g + "," + c.b + "," + Math.min(this.opacity * 1.3, 1) + ")";

      ctx.save();
      ctx.translate(this.x, this.y);
      ctx.rotate(this.angle);

      ctx.strokeStyle = stroke;
      ctx.lineWidth = this.lineWidth;
      ctx.lineCap = "round";

      for (var i = 0; i < this.points.length; i++) {
        var p = this.points[i];
        var ex = Math.cos(p.angle) * p.length;
        var ey = Math.sin(p.angle) * p.length;
        ctx.beginPath();
        ctx.moveTo(0, 0);
        ctx.lineTo(ex, ey);
        ctx.stroke();
      }

      ctx.fillStyle = fill;
      ctx.beginPath();
      ctx.arc(0, 0, this.dotR, 0, Math.PI * 2);
      ctx.fill();

      ctx.restore();
    }
  }

  var nodes = [];
  for (var i = 0; i < NODE_COUNT; i++) {
    nodes.push(new NodeShape());
  }

  function resolveCollisions() {
    for (var i = 0; i < nodes.length; i++) {
      for (var j = i + 1; j < nodes.length; j++) {
        var a = nodes[i], b = nodes[j];
        var dx = b.x - a.x;
        var dy = b.y - a.y;
        var dist = Math.hypot(dx, dy);
        var minDist = a.size + b.size;
        if (dist < minDist && dist > 0) {
          var ang = Math.atan2(dy, dx);
          var overlap = (minDist - dist) * 0.5;
          var ox = Math.cos(ang) * overlap;
          var oy = Math.sin(ang) * overlap;
          a.x -= ox; a.y -= oy;
          b.x += ox; b.y += oy;
          var tvx = a.vx; var tvy = a.vy;
          a.vx = b.vx; a.vy = b.vy;
          b.vx = tvx; b.vy = tvy;
        }
      }
    }
  }

  function animate() {
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    for (var i = 0; i < nodes.length; i++) nodes[i].update();
    resolveCollisions();
    for (var i = 0; i < nodes.length; i++) nodes[i].draw(ctx);
    requestAnimationFrame(animate);
  }
  requestAnimationFrame(animate);
})();
