import { useRef, useState } from 'react';
import { PanResponder, StyleSheet, Text, View, type LayoutChangeEvent } from 'react-native';
import { colors, radius, spacing, typography } from './theme';

/**
 * Slider tự viết.
 *
 * React Native core không còn Slider, và `@react-native-community/slider` là một
 * native module — thêm nó nghĩa là bản xem trước trên trình duyệt không chạy
 * được nữa. Với một điều khiển đơn giản như thế này, tự viết bằng PanResponder
 * rẻ hơn là mất khả năng xem trước.
 *
 * Quan trọng về hiệu năng: `onChange` bắn liên tục trong lúc kéo, và người dùng
 * kéo khoảng 60 lần mỗi giây. Bên nhận PHẢI xử lý được tần suất đó — đây chính
 * là lý do màu được áp trên GPU của thiết bị chứ không gọi server.
 */
export function Slider({
  value,
  onChange,
  label,
  format = v => `${Math.round(v * 100)}%`,
}: {
  value: number;
  onChange: (v: number) => void;
  label?: string;
  format?: (v: number) => string;
}) {
  const [width, setWidth] = useState(0);
  const widthRef = useRef(0);
  const clamp = (v: number) => Math.min(1, Math.max(0, v));

  const responder = useRef(
    PanResponder.create({
      onStartShouldSetPanResponder: () => true,
      onMoveShouldSetPanResponder: () => true,
      onPanResponderGrant: e => {
        if (widthRef.current > 0) onChange(clamp(e.nativeEvent.locationX / widthRef.current));
      },
      onPanResponderMove: (_e, g) => {
        if (widthRef.current > 0) onChange(clamp(g.moveX / widthRef.current));
      },
    }),
  ).current;

  const onLayout = (e: LayoutChangeEvent) => {
    const w = e.nativeEvent.layout.width;
    widthRef.current = w;
    setWidth(w);
  };

  const pct = clamp(value);

  return (
    <View style={s.wrap}>
      {label ? (
        <View style={s.head}>
          <Text style={s.label}>{label}</Text>
          {/* Giá trị dùng chữ đều bề rộng để con số không nhảy vị trí khi kéo. */}
          <Text style={s.value}>{format(pct)}</Text>
        </View>
      ) : null}

      <View style={s.trackHit} onLayout={onLayout} {...responder.panHandlers}>
        <View style={s.track}>
          <View style={[s.fill, { width: `${pct * 100}%` }]} />
        </View>
        <View style={[s.knob, { left: Math.max(0, pct * width - 11) }]} />
      </View>
    </View>
  );
}

const s = StyleSheet.create({
  wrap: { gap: spacing.sm },
  head: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center' },
  label: { ...typography.label },
  value: { ...typography.mono, color: colors.text },

  // Vùng chạm cao hơn thanh trượt nhiều: người dùng thao tác khi đang cầm máy
  // ảnh và thường không nhìn kỹ vào màn hình.
  trackHit: { height: 36, justifyContent: 'center' },
  track: {
    height: 4,
    borderRadius: radius.pill,
    backgroundColor: colors.surfaceRaised,
    overflow: 'hidden',
  },
  fill: { height: 4, backgroundColor: colors.accent },
  knob: {
    position: 'absolute',
    width: 22,
    height: 22,
    borderRadius: 11,
    backgroundColor: colors.text,
    borderWidth: 3,
    borderColor: colors.canvas,
  },
});
