// Serializer

const isMixed = (value) => typeof value === "symbol";

const toHex = (color) => {
  const clamp = (v) => Math.min(255, Math.max(0, Math.round(v * 255)));
  const [r, g, b] = [clamp(color.r), clamp(color.g), clamp(color.b)];
  return `#${[r, g, b].map((v) => v.toString(16).padStart(2, "0")).join("")}`;
};

const serializePaints = (paints) => {
  if (isMixed(paints) || !paints || !Array.isArray(paints)) {
    return isMixed(paints) ? "mixed" : [];
  }
  return paints.map((paint) => {
    if (paint.type === "SOLID") return { type: paint.type, color: toHex(paint.color), opacity: paint.opacity };
    if (paint.type.startsWith("GRADIENT_")) {
      return {
        type: paint.type,
        opacity: paint.opacity,
        stops: paint.gradientStops.map((s) => ({ color: toHex(s.color), alpha: Math.round(s.color.a * 100) / 100, position: Math.round(s.position * 1000) / 1000 })),
        transform: paint.gradientTransform.map((row) => row.map((v) => Math.round(v * 1000) / 1000)),
      };
    }
    return { type: paint.type, opacity: paint.opacity, scaleMode: paint.scaleMode };
  });
};

const serializeEffects = (effects) =>
  effects.map((e) => {
    const out = { type: e.type, radius: e.radius, visible: e.visible };
    if (e.color) Object.assign(out, { color: toHex(e.color), alpha: Math.round(e.color.a * 100) / 100, offset: e.offset, spread: e.spread });
    return out;
  });

const getBounds = (node) => {
  if ("x" in node && "y" in node && "width" in node && "height" in node) {
    return { x: node.x, y: node.y, width: node.width, height: node.height };
  }
  return undefined;
};

const serializeStyles = (node) => {
  const styles = {};
  if ("fills" in node) styles.fills = serializePaints(node.fills);
  if ("strokes" in node) styles.strokes = serializePaints(node.strokes);
  if ("effects" in node && node.effects.length) styles.effects = serializeEffects(node.effects);
  if ("opacity" in node && node.opacity !== 1) styles.opacity = node.opacity;
  if ("blendMode" in node && node.blendMode !== "PASS_THROUGH" && node.blendMode !== "NORMAL") styles.blendMode = node.blendMode;
  if ("cornerRadius" in node) {
    styles.cornerRadius = isMixed(node.cornerRadius) ? "mixed" : node.cornerRadius;
    if (isMixed(node.cornerRadius)) {
      styles.corners = [node.topLeftRadius, node.topRightRadius, node.bottomRightRadius, node.bottomLeftRadius];
    }
  }
  if ("layoutMode" in node && node.layoutMode !== "NONE") {
    styles.layout = { mode: node.layoutMode, primaryAlign: node.primaryAxisAlignItems, counterAlign: node.counterAxisAlignItems, spacing: node.itemSpacing };
  }
  if ("layoutPositioning" in node && node.layoutPositioning === "ABSOLUTE") styles.absolute = true;
  if ("paddingLeft" in node) {
    styles.padding = {
      top: node.paddingTop,
      right: node.paddingRight,
      bottom: node.paddingBottom,
      left: node.paddingLeft,
    };
  }
  return styles;
};

const serializeText = (node, base) => {
  let font;
  if (typeof node.fontName === "symbol") {
    font = "mixed";
  } else if (node.fontName) {
    font = node.fontName.family;
  }
  return Object.assign({}, base, {
    characters: node.characters,
    styles: Object.assign({}, base.styles, {
      fontSize: isMixed(node.fontSize) ? "mixed" : node.fontSize,
      fontFamily: font,
      textAlignHorizontal: isMixed(node.textAlignHorizontal) ? "mixed" : node.textAlignHorizontal,
    }),
  });
};

const serializeNode = (node) => {
  const base = {
    id: node.id,
    name: node.name,
    type: node.type,
    bounds: getBounds(node),
    styles: serializeStyles(node),
  };
  if (node.type === "TEXT") return serializeText(node, base);
  if ("children" in node) {
    return Object.assign({}, base, { children: node.children.map((child) => serializeNode(child)) });
  }
  return base;
};

// trimDepth drops children below the given depth, leaving a childCount marker.
const trimDepth = (serialized, depth, currentDepth = 0) => {
  if (!serialized.children) return serialized;
  if (currentDepth >= depth) {
    return Object.assign({}, serialized, { children: undefined, childCount: serialized.children.length });
  }
  return Object.assign({}, serialized, {
    children: serialized.children.map((child) => trimDepth(child, depth, currentDepth + 1)),
  });
};

const serializeWithOptionalDepth = (node, params) => {
  const serialized = serializeNode(node);
  return params && params.depth != null ? trimDepth(serialized, params.depth) : serialized;
};

// Writers

const parseColor = (value) => {
  const hex = String(value).replace("#", "");
  if (!/^([0-9a-fA-F]{6}|[0-9a-fA-F]{8})$/.test(hex)) throw new Error(`Invalid color: ${value} (use #RRGGBB or #RRGGBBAA)`);
  const channel = (i) => parseInt(hex.slice(i, i + 2), 16) / 255;
  return { color: { r: channel(0), g: channel(2), b: channel(4) }, opacity: hex.length === 8 ? channel(6) : 1 };
};

// toGradient builds a linear gradient. angle is in degrees: 90 runs top to bottom, 0 left to right.
const toGradient = (spec) => {
  const rad = ((spec.angle != null ? spec.angle : 90) * Math.PI) / 180;
  const cos = Math.cos(rad);
  const sin = Math.sin(rad);
  const stops = spec.gradient.map((stop, i, all) => {
    const parsed = parseColor(stop.color);
    return {
      color: Object.assign({}, parsed.color, { a: parsed.opacity }),
      position: stop.position != null ? stop.position : i / Math.max(all.length - 1, 1),
    };
  });
  return {
    type: "GRADIENT_LINEAR",
    gradientStops: stops,
    gradientTransform: [[cos, sin, 0.5 - cos / 2 - sin / 2], [-sin, cos, 0.5 + sin / 2 - cos / 2]],
    opacity: spec.opacity != null ? spec.opacity : 1,
  };
};

// toEffect builds a shadow or blur: {type: DROP_SHADOW|INNER_SHADOW, color, x, y, radius, spread}
// or {type: LAYER_BLUR|BACKGROUND_BLUR, radius}.
const toEffect = (spec) => {
  const type = spec.type || "DROP_SHADOW";
  if (type === "LAYER_BLUR" || type === "BACKGROUND_BLUR") {
    return { type, radius: spec.radius || 0, visible: true };
  }
  const parsed = parseColor(spec.color || "#00000026");
  return {
    type,
    color: Object.assign({}, parsed.color, { a: parsed.opacity }),
    offset: { x: spec.x || 0, y: spec.y || 0 },
    radius: spec.radius || 0,
    spread: spec.spread || 0,
    visible: true,
    blendMode: "NORMAL",
  };
};

// toReaction turns {to, navigation, transition, duration, trigger, timeout} into a Figma Reaction.
// navigation: NAVIGATE (default) | OVERLAY | SWAP | SCROLL_TO | BACK | CLOSE.
// transition: SMART_ANIMATE | DISSOLVE | MOVE_IN | SLIDE_IN | INSTANT (default DISSOLVE). duration in ms.
const toReaction = (spec) => {
  const trigger = spec.trigger === "AFTER_TIMEOUT"
    ? { type: "AFTER_TIMEOUT", timeout: (spec.timeout || 800) / 1000 }
    : { type: spec.trigger || "ON_CLICK" };
  const navigation = spec.navigation || "NAVIGATE";
  if (navigation === "BACK" || navigation === "CLOSE") {
    return { trigger, actions: [{ type: navigation }] };
  }
  const kind = spec.transition || "DISSOLVE";
  const easing = { type: "EASE_OUT" };
  const duration = (spec.duration || 300) / 1000;
  let transition = null;
  if (kind === "SMART_ANIMATE" || kind === "DISSOLVE") transition = { type: kind, easing, duration };
  else if (kind === "MOVE_IN" || kind === "SLIDE_IN") {
    transition = { type: kind, direction: spec.direction || "LEFT", matchLayers: false, easing, duration };
  }
  return {
    trigger,
    actions: [{ type: "NODE", destinationId: spec.to, navigation, transition, preserveScrollPosition: false }],
  };
};

// toPaints accepts "#hex", {color, opacity}, {image: url | imageBase64: png/jpg bytes, scaleMode},
// {gradient: [{color, position}], angle}, or an array of those. Empty array clears.
const toPaints = async (value) => {
  const list = Array.isArray(value) ? value : [value];
  return Promise.all(list.map(async (item) => {
    const spec = typeof item === "string" ? { color: item } : item;
    if (spec.image || spec.imageBase64) {
      const image = spec.imageBase64
        ? figma.createImage(figma.base64Decode(spec.imageBase64))
        : await figma.createImageAsync(spec.image);
      return { type: "IMAGE", imageHash: image.hash, scaleMode: spec.scaleMode || "FILL", opacity: spec.opacity != null ? spec.opacity : 1 };
    }
    if (spec.gradient) return toGradient(spec);
    const parsed = parseColor(spec.color);
    return { type: "SOLID", color: parsed.color, opacity: spec.opacity != null ? spec.opacity : parsed.opacity };
  }));
};

// Fonts stay loaded for the plugin session, so each one is awaited once. Repeated
// loadFontAsync calls still round-trip to the host, which stalls when Figma is backgrounded.
const loadedFonts = new Set();
const loadFont = async (font) => {
  const key = `${font.family}|${font.style}`;
  if (loadedFonts.has(key)) return;
  await figma.loadFontAsync(font);
  loadedFonts.add(key);
};

const loadNodeFonts = async (node) => {
  if (node.characters.length === 0 && !isMixed(node.fontName)) {
    await loadFont(node.fontName);
    return;
  }
  const fonts = node.getRangeAllFontNames(0, node.characters.length);
  await Promise.all(fonts.map(loadFont));
};

// applyProps sets supported properties on a node. Order matters: fonts before
// text, layoutMode before padding/alignment, size before layout sizing.
const applyProps = async (node, props) => {
  const has = (key) => props[key] !== undefined;

  if (has("name")) node.name = props.name;

  if (node.type === "TEXT") {
    if (has("fontFamily") || has("fontStyle")) {
      const fontName = {
        family: props.fontFamily || (isMixed(node.fontName) ? "Inter" : node.fontName.family),
        style: props.fontStyle || "Regular",
      };
      await loadFont(fontName);
      node.fontName = fontName;
    } else {
      await loadNodeFonts(node);
    }
    if (has("characters")) node.characters = String(props.characters);
    if (has("fontSize")) node.fontSize = props.fontSize;
    if (has("textAlignHorizontal")) node.textAlignHorizontal = props.textAlignHorizontal;
    if (has("textAlignVertical")) node.textAlignVertical = props.textAlignVertical;
    if (has("textAutoResize")) node.textAutoResize = props.textAutoResize;
    if (has("textDecoration")) node.textDecoration = props.textDecoration;
    if (has("lineHeight")) node.lineHeight = typeof props.lineHeight === "number" ? { unit: "PIXELS", value: props.lineHeight } : props.lineHeight;
    if (has("letterSpacing")) node.letterSpacing = typeof props.letterSpacing === "number" ? { unit: "PIXELS", value: props.letterSpacing } : props.letterSpacing;
  }

  if (has("layoutMode")) node.layoutMode = props.layoutMode;
  if (has("layoutWrap")) node.layoutWrap = props.layoutWrap;
  if (has("itemSpacing")) node.itemSpacing = props.itemSpacing;
  if (has("counterAxisSpacing")) node.counterAxisSpacing = props.counterAxisSpacing;
  if (has("padding")) {
    const p = typeof props.padding === "number"
      ? { top: props.padding, right: props.padding, bottom: props.padding, left: props.padding }
      : props.padding;
    if (p.top != null) node.paddingTop = p.top;
    if (p.right != null) node.paddingRight = p.right;
    if (p.bottom != null) node.paddingBottom = p.bottom;
    if (p.left != null) node.paddingLeft = p.left;
  }
  if (has("primaryAxisAlignItems")) node.primaryAxisAlignItems = props.primaryAxisAlignItems;
  if (has("counterAxisAlignItems")) node.counterAxisAlignItems = props.counterAxisAlignItems;
  if (has("primaryAxisSizingMode")) node.primaryAxisSizingMode = props.primaryAxisSizingMode;
  if (has("counterAxisSizingMode")) node.counterAxisSizingMode = props.counterAxisSizingMode;
  if (has("clipsContent")) node.clipsContent = props.clipsContent;

  if (has("width") || has("height")) {
    if (!("resize" in node)) throw new Error(`Node ${node.id} (${node.type}) cannot be resized`);
    node.resize(has("width") ? props.width : node.width, has("height") ? props.height : node.height);
  }
  // layoutPositioning first: x/y are ignored while a node is still auto-positioned.
  if (has("layoutPositioning")) node.layoutPositioning = props.layoutPositioning;
  if (has("x")) node.x = props.x;
  if (has("y")) node.y = props.y;
  if (has("rotation")) node.rotation = props.rotation;
  if (has("layoutSizingHorizontal")) node.layoutSizingHorizontal = props.layoutSizingHorizontal;
  if (has("layoutSizingVertical")) node.layoutSizingVertical = props.layoutSizingVertical;

  if (has("fills")) node.fills = await toPaints(props.fills);
  if (has("strokes")) node.strokes = await toPaints(props.strokes);
  if (has("strokeWeight")) node.strokeWeight = props.strokeWeight;
  if (has("strokeAlign")) node.strokeAlign = props.strokeAlign;
  if (has("dashPattern")) node.dashPattern = props.dashPattern;
  if (has("cornerRadius")) node.cornerRadius = props.cornerRadius;
  for (const corner of ["topLeftRadius", "topRightRadius", "bottomLeftRadius", "bottomRightRadius"]) {
    if (has(corner)) node[corner] = props[corner];
  }
  // arcData angles are in degrees (0 = 3 o'clock, clockwise); innerRadius is 0..1.
  if (has("arcData")) {
    const toRad = (deg) => (deg * Math.PI) / 180;
    node.arcData = {
      startingAngle: toRad(props.arcData.startingAngle || 0),
      endingAngle: toRad(props.arcData.endingAngle != null ? props.arcData.endingAngle : 360),
      innerRadius: props.arcData.innerRadius || 0,
    };
  }
  if (has("effects")) node.effects = props.effects.map(toEffect);
  // TEXT: colour character ranges, e.g. [{start: 0, end: 12, fills: "#0A0E1F"}].
  if (has("fillRanges")) {
    for (const r of props.fillRanges) node.setRangeFills(r.start, r.end, await toPaints(r.fills));
  }
  if (has("reactions")) await node.setReactionsAsync(props.reactions.map(toReaction));
  // Marks a top-level frame as a prototype flow start on its page.
  if (has("flowStartName")) {
    const page = node.parent;
    const others = page.flowStartingPoints.filter((p) => p.nodeId !== node.id);
    page.flowStartingPoints = props.flowStartName ? [...others, { nodeId: node.id, name: props.flowStartName }] : others;
  }
  if (has("opacity")) node.opacity = props.opacity;
  if (has("visible")) node.visible = props.visible;
  if (has("locked")) node.locked = props.locked;
};

const getParent = async (parentId) => {
  if (!parentId) return figma.currentPage;
  const parent = await figma.getNodeByIdAsync(parentId);
  if (!parent || !("appendChild" in parent)) throw new Error(`Parent not found or cannot have children: ${parentId}`);
  return parent;
};

const insertInto = (parent, node, index) => {
  if (index != null) parent.insertChild(index, node);
  else parent.appendChild(node);
};

const createByType = {
  FRAME: () => figma.createFrame(),
  RECTANGLE: () => figma.createRectangle(),
  ELLIPSE: () => figma.createEllipse(),
  LINE: () => figma.createLine(),
  TEXT: () => figma.createText(),
  COMPONENT: () => figma.createComponent(),
};

// createTree creates a node (and nested children) under parent.
const createTree = async (spec, parent) => {
  const create = createByType[spec.type];
  if (!create) throw new Error(`Unsupported node type: ${spec.type}. Use one of ${Object.keys(createByType).join(", ")}`);
  const node = create();
  try {
    if (node.type === "TEXT") await loadFont(node.fontName);
    insertInto(parent, node, spec.index);
    const props = Object.assign({}, spec);
    delete props.type;
    delete props.children;
    delete props.index;
    await applyProps(node, props);
    if (spec.children) {
      for (const child of spec.children) await createTree(child, node);
    }
  } catch (err) {
    node.remove();
    throw err;
  }
  return node;
};

const requireNode = async (nodeId) => {
  const node = await figma.getNodeByIdAsync(nodeId);
  if (!node || node.type === "DOCUMENT" || node.type === "PAGE") throw new Error(`Node not found or not editable: ${nodeId}`);
  return node;
};

const summarize = (node) => ({ id: node.id, name: node.name, type: node.type, bounds: getBounds(node) });

// Plugin core

const sendStatus = () => {
  figma.ui.postMessage({
    type: "plugin-status",
    payload: {
      fileName: figma.root.name,
      selectionCount: figma.currentPage.selection.length,
    },
  });
};

const serializeVariableValue = (value) => {
  if (typeof value === "object" && value !== null) {
    if ("type" in value && value.type === "VARIABLE_ALIAS") {
      return { type: "VARIABLE_ALIAS", id: value.id };
    }
    if ("r" in value && "g" in value && "b" in value) {
      return { type: "COLOR", r: value.r, g: value.g, b: value.b, a: "a" in value ? value.a : 1 };
    }
  }
  return value;
};

const handleRequest = async (request) => {
  try {
    switch (request.type) {
      case "get_document":
        return { type: request.type, requestId: request.requestId, data: serializeNode(figma.currentPage) };

      case "get_selection":
        return {
          type: request.type,
          requestId: request.requestId,
          data: figma.currentPage.selection.map((node) => serializeNode(node)),
        };

      case "get_node": {
        const nodeId = request.nodeIds && request.nodeIds[0];
        if (!nodeId) throw new Error("nodeIds is required for get_node");
        const node = await figma.getNodeByIdAsync(nodeId);
        if (!node || node.type === "DOCUMENT") throw new Error(`Node not found: ${nodeId}`);
        return { type: request.type, requestId: request.requestId, data: serializeWithOptionalDepth(node, request.params) };
      }

      case "get_styles": {
        const [paintStyles, textStyles, effectStyles, gridStyles] = await Promise.all([
          figma.getLocalPaintStylesAsync(),
          figma.getLocalTextStylesAsync(),
          figma.getLocalEffectStylesAsync(),
          figma.getLocalGridStylesAsync(),
        ]);
        return {
          type: request.type,
          requestId: request.requestId,
          data: {
            paints: paintStyles.map((s) => ({ id: s.id, name: s.name, paints: s.paints })),
            text: textStyles.map((s) => ({ id: s.id, name: s.name, fontSize: s.fontSize, fontName: s.fontName })),
            effects: effectStyles.map((s) => ({ id: s.id, name: s.name, effects: s.effects })),
            grids: gridStyles.map((s) => ({ id: s.id, name: s.name, layoutGrids: s.layoutGrids })),
          },
        };
      }

      case "get_metadata":
        return {
          type: request.type,
          requestId: request.requestId,
          data: {
            fileName: figma.root.name,
            currentPageId: figma.currentPage.id,
            currentPageName: figma.currentPage.name,
            pageCount: figma.root.children.length,
            pages: figma.root.children.map((page) => ({ id: page.id, name: page.name })),
          },
        };

      case "get_design_context": {
        const depth = (request.params && request.params.depth != null) ? request.params.depth : 2;
        const serializeWithDepth = async (node, currentDepth) => {
          const serialized = serializeNode(node);
          if (currentDepth >= depth && serialized.children) {
            return Object.assign({}, serialized, { children: undefined, childCount: node.children ? node.children.length : 0 });
          }
          if (serialized.children) {
            const childNodes = await Promise.all(
              serialized.children.map((child) => figma.getNodeByIdAsync(child.id))
            );
            const serializedChildren = await Promise.all(
              childNodes
                .filter((n) => n !== null && n.type !== "DOCUMENT")
                .map((n) => serializeWithDepth(n, currentDepth + 1))
            );
            return Object.assign({}, serialized, { children: serializedChildren });
          }
          return serialized;
        };
        const selection = figma.currentPage.selection;
        const contextNodes =
          selection.length > 0
            ? await Promise.all(selection.map((node) => serializeWithDepth(node, 0)))
            : [await serializeWithDepth(figma.currentPage, 0)];
        return {
          type: request.type,
          requestId: request.requestId,
          data: {
            fileName: figma.root.name,
            currentPage: { id: figma.currentPage.id, name: figma.currentPage.name },
            selectionCount: selection.length,
            context: contextNodes,
          },
        };
      }

      case "get_variable_defs": {
        const collections = await figma.variables.getLocalVariableCollectionsAsync();
        const variableData = await Promise.all(
          collections.map(async (collection) => {
            const variables = await Promise.all(
              collection.variableIds.map((id) => figma.variables.getVariableByIdAsync(id))
            );
            return {
              id: collection.id,
              name: collection.name,
              modes: collection.modes.map((mode) => ({ modeId: mode.modeId, name: mode.name })),
              variables: variables
                .filter((v) => v !== null)
                .map((variable) => ({
                  id: variable.id,
                  name: variable.name,
                  resolvedType: variable.resolvedType,
                  valuesByMode: Object.fromEntries(
                    Object.entries(variable.valuesByMode).map(([modeId, value]) => [
                      modeId,
                      serializeVariableValue(value),
                    ])
                  ),
                })),
            };
          })
        );
        return { type: request.type, requestId: request.requestId, data: { collections: variableData } };
      }

      case "get_screenshot": {
        const format = (request.params && request.params.format) ? request.params.format : "PNG";
        const scale = (request.params && request.params.scale != null) ? request.params.scale : 2;
        let targetNodes;
        if (request.nodeIds && request.nodeIds.length > 0) {
          const nodes = await Promise.all(request.nodeIds.map((id) => figma.getNodeByIdAsync(id)));
          targetNodes = nodes.filter((n) => n !== null && n.type !== "DOCUMENT" && n.type !== "PAGE");
        } else {
          targetNodes = figma.currentPage.selection.slice();
        }
        if (targetNodes.length === 0) throw new Error("No nodes to export. Select nodes or provide nodeIds.");
        const exports = await Promise.all(
          targetNodes.map(async (node) => {
            const settings =
              format === "SVG" ? { format: "SVG" }
              : format === "PDF" ? { format: "PDF" }
              : format === "JPG" ? { format: "JPG", constraint: { type: "SCALE", value: scale } }
              : { format: "PNG", constraint: { type: "SCALE", value: scale } };
            const bytes = await node.exportAsync(settings);
            const base64 = figma.base64Encode(bytes);
            return { nodeId: node.id, nodeName: node.name, format, base64, width: node.width, height: node.height };
          })
        );
        return { type: request.type, requestId: request.requestId, data: { exports } };
      }

      case "get_nodes_info": {
        if (!request.nodeIds || request.nodeIds.length === 0) throw new Error("nodeIds is required for get_nodes_info");
        const nodes = await Promise.all(request.nodeIds.map((id) => figma.getNodeByIdAsync(id)));
        return {
          type: request.type,
          requestId: request.requestId,
          data: nodes
            .filter((n) => n !== null && n.type !== "DOCUMENT")
            .map((n) => serializeWithOptionalDepth(n, request.params)),
        };
      }

      case "get_local_components": {
        const pages = figma.root.children;
        const allComponents = [];
        for (let i = 0; i < pages.length; i++) {
          const page = pages[i];
          await page.loadAsync();
          const pageComponents = page.findAllWithCriteria({ types: ["COMPONENT"] });
          for (const component of pageComponents) {
            allComponents.push({
              id: component.id,
              name: component.name,
              key: "key" in component ? component.key : null,
            });
          }
          figma.ui.postMessage({
            type: "progress_update",
            requestId: request.requestId,
            progress: Math.round(((i + 1) / pages.length) * 90) + 1,
            message: `Scanned ${page.name}: ${allComponents.length} components so far`,
          });
          await new Promise((r) => setTimeout(r, 0));
        }
        return { type: request.type, requestId: request.requestId, data: { count: allComponents.length, components: allComponents } };
      }

      case "get_annotations": {
        const nodeId = request.params && request.params.nodeId;
        const nodeAnnotations = (n) => {
          const anns = n.annotations;
          return Array.isArray(anns) ? anns : null;
        };
        if (nodeId) {
          const node = await figma.getNodeByIdAsync(nodeId);
          if (!node) throw new Error(`Node not found: ${nodeId}`);
          const mergedAnnotations = [];
          const collect = async (n) => {
            const anns = nodeAnnotations(n);
            if (anns) for (const a of anns) mergedAnnotations.push({ nodeId: n.id, annotation: a });
            if ("children" in n) for (const child of n.children) await collect(child);
          };
          await collect(node);
          return { type: request.type, requestId: request.requestId, data: { nodeId: node.id, name: node.name, annotations: mergedAnnotations } };
        } else {
          const annotated = [];
          const processNode = async (n) => {
            const anns = nodeAnnotations(n);
            if (anns && anns.length > 0) annotated.push({ nodeId: n.id, name: n.name, annotations: anns });
            if ("children" in n) for (const child of n.children) await processNode(child);
          };
          await processNode(figma.currentPage);
          return { type: request.type, requestId: request.requestId, data: { annotatedNodes: annotated } };
        }
      }

      case "scan_text_nodes": {
        const nodeId = request.params && request.params.nodeId;
        if (!nodeId) throw new Error("nodeId is required for scan_text_nodes");
        const root = await figma.getNodeByIdAsync(nodeId);
        if (!root) throw new Error(`Node not found: ${nodeId}`);
        const textNodes = [];
        const findText = async (n) => {
          if (n.type === "TEXT") {
            textNodes.push({ id: n.id, name: n.name, characters: n.characters, fontSize: n.fontSize, fontName: n.fontName });
          }
          if ("children" in n) for (const child of n.children) await findText(child);
        };
        figma.ui.postMessage({ type: "progress_update", requestId: request.requestId, progress: 10, message: "Scanning text nodes..." });
        await new Promise((r) => setTimeout(r, 0));
        await findText(root);
        return { type: request.type, requestId: request.requestId, data: { count: textNodes.length, textNodes } };
      }

      case "scan_nodes_by_types": {
        const nodeId = request.params && request.params.nodeId;
        const types = (request.params && request.params.types) ? request.params.types : [];
        if (!nodeId) throw new Error("nodeId is required for scan_nodes_by_types");
        if (types.length === 0) throw new Error("types must be a non-empty array");
        const root = await figma.getNodeByIdAsync(nodeId);
        if (!root) throw new Error(`Node not found: ${nodeId}`);
        const matchingNodes = [];
        const findByTypes = async (n) => {
          if ("visible" in n && !n.visible) return;
          if (types.includes(n.type)) {
            matchingNodes.push({
              id: n.id,
              name: n.name,
              type: n.type,
              bbox: { x: "x" in n ? n.x : 0, y: "y" in n ? n.y : 0, width: "width" in n ? n.width : 0, height: "height" in n ? n.height : 0 },
            });
          }
          if ("children" in n) for (const child of n.children) await findByTypes(child);
        };
        figma.ui.postMessage({ type: "progress_update", requestId: request.requestId, progress: 10, message: `Scanning for types: ${types.join(", ")}...` });
        await new Promise((r) => setTimeout(r, 0));
        await findByTypes(root);
        return { type: request.type, requestId: request.requestId, data: { count: matchingNodes.length, matchingNodes, searchedTypes: types } };
      }

      case "create_nodes": {
        const parent = await getParent(request.params.parentId);
        const created = [];
        // Large trees can outlast the 30s bridge timeout; progress updates extend it.
        const heartbeat = setInterval(() => {
          figma.ui.postMessage({ type: "progress_update", requestId: request.requestId, progress: 50, message: "Creating nodes..." });
        }, 10000);
        try {
          for (const spec of request.params.nodes) created.push(await createTree(spec, parent));
        } finally {
          clearInterval(heartbeat);
        }
        figma.commitUndo();
        return { type: request.type, requestId: request.requestId, data: { created: created.map((n) => serializeNode(n)) } };
      }

      case "update_nodes": {
        const updated = [];
        for (const update of request.params.updates) {
          const node = await requireNode(update.nodeId);
          const props = Object.assign({}, update);
          delete props.nodeId;
          delete props.parentId;
          delete props.index;
          if (update.parentId) insertInto(await getParent(update.parentId), node, update.index);
          await applyProps(node, props);
          updated.push(summarize(node));
        }
        figma.commitUndo();
        return { type: request.type, requestId: request.requestId, data: { updated } };
      }

      case "delete_nodes": {
        const deleted = [];
        for (const id of request.nodeIds) {
          const node = await requireNode(id);
          deleted.push(summarize(node));
          node.remove();
        }
        figma.commitUndo();
        return { type: request.type, requestId: request.requestId, data: { deleted } };
      }

      case "clone_node": {
        const source = await requireNode(request.nodeIds[0]);
        const clone = source.clone();
        const params = request.params || {};
        if (params.parentId) insertInto(await getParent(params.parentId), clone, params.index);
        await applyProps(clone, { x: params.x, y: params.y, name: params.name });
        figma.commitUndo();
        return { type: request.type, requestId: request.requestId, data: summarize(clone) };
      }

      case "create_instance": {
        const params = request.params;
        let component;
        if (params.componentKey) {
          component = await figma.importComponentByKeyAsync(params.componentKey);
        } else {
          component = await figma.getNodeByIdAsync(params.componentId);
          if (!component || component.type !== "COMPONENT") throw new Error(`Component not found: ${params.componentId}`);
        }
        const instance = component.createInstance();
        insertInto(await getParent(params.parentId), instance, params.index);
        await applyProps(instance, { x: params.x, y: params.y, name: params.name });
        figma.commitUndo();
        return { type: request.type, requestId: request.requestId, data: summarize(instance) };
      }

      case "set_selection": {
        const nodes = await Promise.all(request.nodeIds.map((id) => requireNode(id)));
        figma.currentPage.selection = nodes;
        if (nodes.length > 0) figma.viewport.scrollAndZoomIntoView(nodes);
        return { type: request.type, requestId: request.requestId, data: { selected: nodes.map(summarize) } };
      }

      default:
        throw new Error(`Unknown request type: ${request.type}`);
    }
  } catch (error) {
    return {
      type: request.type,
      requestId: request.requestId,
      error: error instanceof Error ? error.message : String(error),
    };
  }
};

figma.showUI(__html__, { width: 320, height: 180 });
sendStatus();

figma.on("selectionchange", () => {
  sendStatus();
});

figma.ui.onmessage = async (message) => {
  if (message.type === "ui-ready") {
    sendStatus();
    return;
  }
  if (message.type === "server-request") {
    const response = await handleRequest(message.payload);
    try {
      figma.ui.postMessage(response);
    } catch (err) {
      figma.ui.postMessage({
        type: response.type,
        requestId: response.requestId,
        error: err instanceof Error ? err.message : String(err),
      });
    }
  }
};
